// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2024 Renesas Electronics Corporation.
// Copyright (C) 2024 EPAM Systems, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package iamclient

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/aostypes"
	"github.com/aosedge/aos_common/api/cloudprotocol"
	pb "github.com/aosedge/aos_common/api/iamanager"
	"github.com/aosedge/aos_common/utils/cryptutils"
	"github.com/aosedge/aos_common/utils/grpchelpers"
	"github.com/aosedge/aos_common/utils/pbconvert"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const (
	iamRequestTimeout    = 30 * time.Second
	iamReconnectInterval = 10 * time.Second
)

const (
	iamCertType = "iam"
)

var errProtectedConnDisabled = aoserrors.New("protected connection disabled")

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// Client IAM client instance.
type Client struct {
	sync.Mutex

	publicURL, protectedURL string
	certStorage             string
	sender                  Sender
	cryptocontext           *cryptutils.CryptoContext
	insecure                bool

	nodeID     string
	systemID   string
	isMainNode bool

	publicConnection    *grpchelpers.GRPCConn
	protectedConnection *grpc.ClientConn

	publicService            pb.IAMPublicServiceClient
	identService             pb.IAMPublicIdentityServiceClient
	certificateService       pb.IAMCertificateServiceClient
	provisioningService      pb.IAMProvisioningServiceClient
	publicNodesService       pb.IAMPublicNodesServiceClient
	nodesService             pb.IAMNodesServiceClient
	publicPermissionsService pb.IAMPublicPermissionsServiceClient
	permissionsService       pb.IAMPermissionsServiceClient

	nodeInfoSubs  *nodeInfoChangeSub
	subjectsSubs  *subjectsChangeSub
	certChangeSub map[string]*certChangeSub

	closeChannel   chan struct{}
	reconnectWG    sync.WaitGroup
	isReconnecting atomic.Bool
}

// Sender provides API to send messages to the cloud.
type Sender interface {
	SendIssueUnitCerts(requests []cloudprotocol.IssueCertData) (err error)
	SendInstallCertsConfirmation(confirmations []cloudprotocol.InstallCertData) (err error)
	SendStartProvisioningResponse(response cloudprotocol.StartProvisioningResponse) (err error)
	SendFinishProvisioningResponse(response cloudprotocol.FinishProvisioningResponse) (err error)
	SendDeprovisioningResponse(response cloudprotocol.DeprovisioningResponse) (err error)
}

// CertificateProvider provides certificate info.
type CertificateProvider interface {
	GetCertSerial(certURL string) (serial string, err error)
}

// certSubscription generic subscription for IAM public messages.
type iamSubscription[grpcStream *pb.IAMPublicService_SubscribeCertChangedClient |
	*pb.IAMPublicNodesService_SubscribeNodeChangedClient |
	*pb.IAMPublicIdentityService_SubscribeSubjectsChangedClient, T any] struct {
	grpcStream grpcStream
	listeners  []chan T
	stopWG     sync.WaitGroup
}

type (
	nodeInfoChangeSub = iamSubscription[*pb.IAMPublicNodesService_SubscribeNodeChangedClient, cloudprotocol.NodeInfo]
	certChangeSub     = iamSubscription[*pb.IAMPublicService_SubscribeCertChangedClient, *pb.CertInfo]
	subjectsChangeSub = iamSubscription[*pb.IAMPublicIdentityService_SubscribeSubjectsChangedClient, []string]
)

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// New creates new IAM client.
func New(
	publicURL, protectedURL string, certStorage string, sender Sender,
	cryptocontext *cryptutils.CryptoContext, insecure bool,
) (client *Client, err error) {
	localClient := &Client{
		publicURL:        publicURL,
		protectedURL:     protectedURL,
		certStorage:      certStorage,
		sender:           sender,
		cryptocontext:    cryptocontext,
		insecure:         insecure,
		publicConnection: grpchelpers.NewGRPCConn(),
		nodeInfoSubs: &nodeInfoChangeSub{
			listeners: make([]chan cloudprotocol.NodeInfo, 0),
		},
		subjectsSubs: &subjectsChangeSub{
			listeners: make([]chan []string, 0),
		},
		certChangeSub: make(map[string]*certChangeSub),

		closeChannel: make(chan struct{}, 1),
	}

	localClient.isReconnecting.Store(true) // disable reconnecting on Init

	defer func() {
		if err != nil {
			localClient.Close()
		} else {
			localClient.isReconnecting.Store(false)
		}
	}()

	if err = localClient.openGRPCConnection(); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	nodeInfo, err := localClient.GetCurrentNodeInfo()
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	localClient.nodeID = nodeInfo.NodeID
	localClient.isMainNode = nodeInfo.IsMainNode()

	if localClient.isMainNode {
		if localClient.systemID, err = localClient.getSystemID(); err != nil {
			return nil, aoserrors.Wrap(err)
		}
	}

	if localClient.isMainNode {
		if err = localClient.subscribeNodeInfoChange(); err != nil {
			log.Error("Failed subscribe on NodeInfo change")

			return nil, aoserrors.Wrap(err)
		}

		if err = localClient.subscribeUnitSubjectsChange(); err != nil {
			log.Error("Failed subscribe on UnitSubject change")

			return nil, aoserrors.Wrap(err)
		}
	}

	if !insecure && localClient.isProtectedConnEnabled() {
		var ch <-chan *pb.CertInfo

		if ch, err = localClient.SubscribeCertChanged(certStorage); err != nil {
			return nil, aoserrors.Wrap(err)
		}

		go localClient.processCertChange(ch)
	}

	return localClient, nil
}

// GetNodeID returns node ID.
func (client *Client) GetNodeID() string {
	return client.nodeID
}

// GetSystemID returns system ID.
func (client *Client) GetSystemID() (systemID string) {
	return client.systemID
}

// GetNodeInfo returns node info.
func (client *Client) GetNodeInfo(nodeID string) (nodeInfo cloudprotocol.NodeInfo, err error) {
	client.Lock()
	defer client.Unlock()

	log.WithField("nodeID", nodeID).Debug("Get node info")

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	request := &pb.GetNodeInfoRequest{NodeId: nodeID}

	response, err := client.publicNodesService.GetNodeInfo(ctx, request)
	if err != nil {
		return cloudprotocol.NodeInfo{}, aoserrors.Wrap(err)
	}

	return pbconvert.NodeInfoFromPB(response), nil
}

// GetAllNodeIDs returns node ids.
func (client *Client) GetAllNodeIDs() (nodeIDs []string, err error) {
	client.Lock()
	defer client.Unlock()

	log.Debug("Get all node ids")

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.publicNodesService.GetAllNodeIDs(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return response.GetIds(), err
}

// SubscribeNodeInfoChange subscribes client on NodeInfoChange events.
func (client *Client) SubscribeNodeInfoChange() <-chan cloudprotocol.NodeInfo {
	client.Lock()
	defer client.Unlock()

	log.Debug("Subscribe on node info change event")

	ch := make(chan cloudprotocol.NodeInfo, 1)
	client.nodeInfoSubs.listeners = append(client.nodeInfoSubs.listeners, ch)

	return ch
}

// RenewCertificatesNotification notification about certificate updates.
func (client *Client) RenewCertificatesNotification(secrets cloudprotocol.UnitSecrets,
	certInfo []cloudprotocol.RenewCertData,
) (err error) {
	client.Lock()
	defer client.Unlock()

	if !client.isProtectedConnEnabled() {
		return errProtectedConnDisabled
	}

	newCerts := make([]cloudprotocol.IssueCertData, 0, len(certInfo))

	for _, cert := range certInfo {
		log.WithFields(log.Fields{
			"type": cert.Type, "serial": cert.Serial, "nodeID": cert.NodeID, "validTill": cert.ValidTill,
		}).Debug("Renew certificate")

		ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
		defer cancel()

		pwd, ok := secrets.Nodes[cert.NodeID]
		if !ok {
			return aoserrors.New("not found password for node: " + cert.NodeID)
		}

		request := &pb.CreateKeyRequest{Type: cert.Type, Password: pwd, NodeId: cert.NodeID}

		response, err := client.certificateService.CreateKey(ctx, request)
		if err != nil {
			return aoserrors.Wrap(err)
		}

		newCerts = append(newCerts, cloudprotocol.IssueCertData{
			Type: response.GetType(), Csr: response.GetCsr(), NodeID: cert.NodeID,
		})
	}

	if len(newCerts) == 0 {
		return nil
	}

	if client.sender == nil {
		return nil
	}

	if err := client.sender.SendIssueUnitCerts(newCerts); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// InstallCertificates applies new issued certificates.
func (client *Client) InstallCertificates(
	certInfo []cloudprotocol.IssuedCertData, certProvider CertificateProvider,
) error {
	client.Lock()
	defer client.Unlock()

	if !client.isProtectedConnEnabled() {
		return errProtectedConnDisabled
	}

	confirmations := make([]cloudprotocol.InstallCertData, len(certInfo))

	// IAM cert type of secondary nodes should be sent the latest among certificates for that node.
	// And IAM certificate for the main node should be send in the end. Otherwise IAM client/server
	// restart will fail the following certificates to apply.
	slices.SortStableFunc(certInfo, func(a, b cloudprotocol.IssuedCertData) int {
		if client.isMainNode && a.NodeID == client.nodeID && a.Type == iamCertType {
			return 1
		}

		if client.isMainNode && b.NodeID == client.nodeID && b.Type == iamCertType {
			return -1
		}

		if client.isMainNode && a.NodeID == client.nodeID {
			return 1
		}

		if client.isMainNode && b.NodeID == client.nodeID {
			return -1
		}

		if a.NodeID == b.NodeID {
			if a.Type == iamCertType {
				return 1
			}

			if b.Type == iamCertType {
				return -1
			}

			return 0
		}

		return strings.Compare(a.NodeID, b.NodeID)
	})

	for i, cert := range certInfo {
		log.WithFields(log.Fields{"type": cert.Type}).Debug("Install certificate")

		ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
		defer cancel()

		request := &pb.ApplyCertRequest{Type: cert.Type, Cert: cert.CertificateChain, NodeId: cert.NodeID}
		certConfirmation := cloudprotocol.InstallCertData{Type: cert.Type, NodeID: cert.NodeID}

		response, err := client.certificateService.ApplyCert(ctx, request)
		if err == nil {
			certConfirmation.Status = "installed"
		} else {
			certConfirmation.Status = "not installed"
			certConfirmation.Description = err.Error()

			log.WithFields(log.Fields{"type": cert.Type}).Errorf("Can't install certificate: %s", err)
		}

		if response != nil {
			certConfirmation.Serial = response.GetSerial()
		}

		confirmations[i] = certConfirmation
	}

	if len(confirmations) == 0 {
		return nil
	}

	if client.sender == nil {
		return nil
	}

	if err := client.sender.SendInstallCertsConfirmation(confirmations); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// StartProvisioning starts provisioning.
func (client *Client) StartProvisioning(nodeID, password string) (err error) {
	client.Lock()
	defer client.Unlock()

	log.WithField("nodeID", nodeID).Debug("Start provisioning")

	if !client.isProtectedConnEnabled() {
		return errProtectedConnDisabled
	}

	var (
		errorInfo *cloudprotocol.ErrorInfo
		csrs      []cloudprotocol.IssueCertData
	)

	defer func() {
		if client.sender != nil {
			errSend := client.sender.SendStartProvisioningResponse(cloudprotocol.StartProvisioningResponse{
				MessageType: cloudprotocol.StartProvisioningResponseMessageType,
				NodeID:      nodeID,
				ErrorInfo:   errorInfo,
				CSRs:        csrs,
			})
			if errSend != nil && err == nil {
				err = aoserrors.Wrap(errSend)
			}
		}
	}()

	errorInfo = client.startProvisioning(nodeID, password)
	if errorInfo != nil {
		return aoserrors.New(errorInfo.Message)
	}

	csrs, errorInfo = client.createKeys(nodeID, password)
	if errorInfo != nil {
		return aoserrors.New(errorInfo.Message)
	}

	return nil
}

// FinishProvisioning starts provisioning.
func (client *Client) FinishProvisioning(
	nodeID, password string, certificates []cloudprotocol.IssuedCertData,
) (err error) {
	client.Lock()
	defer client.Unlock()

	log.WithField("nodeID", nodeID).Debug("Finish provisioning")

	if !client.isProtectedConnEnabled() {
		return errProtectedConnDisabled
	}

	var errorInfo *cloudprotocol.ErrorInfo

	defer func() {
		if client.sender != nil {
			errSend := client.sender.SendFinishProvisioningResponse(cloudprotocol.FinishProvisioningResponse{
				MessageType: cloudprotocol.FinishProvisioningResponseMessageType,
				NodeID:      nodeID,
				ErrorInfo:   errorInfo,
			})
			if errSend != nil && err == nil {
				err = aoserrors.Wrap(errSend)
			}
		}
	}()

	errorInfo = client.applyCertificates(nodeID, certificates)
	if errorInfo != nil {
		return aoserrors.New(errorInfo.Message)
	}

	errorInfo = client.finishProvisioning(nodeID, password)
	if errorInfo != nil {
		return aoserrors.New(errorInfo.Message)
	}

	return nil
}

// Deprovision deprovisions node.
func (client *Client) Deprovision(nodeID, password string) (err error) {
	client.Lock()
	defer client.Unlock()

	log.WithField("nodeID", nodeID).Debug("Deprovision node")

	if !client.isProtectedConnEnabled() {
		return errProtectedConnDisabled
	}

	var errorInfo *cloudprotocol.ErrorInfo

	defer func() {
		if client.sender != nil {
			errSend := client.sender.SendDeprovisioningResponse(cloudprotocol.DeprovisioningResponse{
				MessageType: cloudprotocol.DeprovisioningResponseMessageType,
				NodeID:      nodeID,
				ErrorInfo:   errorInfo,
			})
			if errSend != nil && err == nil {
				err = aoserrors.Wrap(errSend)
			}
		}
	}()

	if client.isMainNode && nodeID == client.nodeID {
		err = aoserrors.New("Can't deprovision main node")
		errorInfo = &cloudprotocol.ErrorInfo{
			Message: err.Error(),
		}

		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.provisioningService.Deprovision(
		ctx, &pb.DeprovisionRequest{NodeId: nodeID, Password: password})
	if err != nil {
		errorInfo = &cloudprotocol.ErrorInfo{Message: err.Error()}

		return aoserrors.Wrap(err)
	}

	errorInfo = pbconvert.ErrorInfoFromPB(response.GetError())
	if errorInfo != nil {
		return aoserrors.New(errorInfo.Message)
	}

	return nil
}

// PauseNode pauses node.
func (client *Client) PauseNode(nodeID string) error {
	client.Lock()
	defer client.Unlock()

	log.WithField("nodeID", nodeID).Debug("Pause node")

	if !client.isProtectedConnEnabled() {
		return errProtectedConnDisabled
	}

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.nodesService.PauseNode(ctx, &pb.PauseNodeRequest{NodeId: nodeID})
	if err != nil {
		return aoserrors.Wrap(err)
	}

	errorInfo := pbconvert.ErrorInfoFromPB(response.GetError())
	if errorInfo != nil {
		return aoserrors.New(errorInfo.Message)
	}

	return nil
}

// ResumeNode resumes node.
func (client *Client) ResumeNode(nodeID string) error {
	client.Lock()
	defer client.Unlock()

	log.WithField("nodeID", nodeID).Debug("Resume node")

	if !client.isProtectedConnEnabled() {
		return errProtectedConnDisabled
	}

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.nodesService.ResumeNode(ctx, &pb.ResumeNodeRequest{NodeId: nodeID})
	if err != nil {
		return aoserrors.Wrap(err)
	}

	errorInfo := pbconvert.ErrorInfoFromPB(response.GetError())
	if errorInfo != nil {
		return aoserrors.New(errorInfo.Message)
	}

	return nil
}

// Close closes IAM client.
func (client *Client) Close() error {
	close(client.closeChannel)
	client.reconnectWG.Wait()

	client.Lock()
	defer client.Unlock()

	// disable reconnect
	client.isReconnecting.Store(true)

	client.closeGRPCConnection()
	client.publicConnection.Close()

	log.Debug("Disconnected from IAM")

	return nil
}

// GetCertificate gets certificate by issuer.
func (client *Client) GetCertificate(
	certType string, issuer []byte, serial string,
) (certURL, keyURL string, err error) {
	client.Lock()
	defer client.Unlock()

	log.WithFields(log.Fields{
		"type":   certType,
		"issuer": base64.StdEncoding.EncodeToString(issuer),
		"serial": serial,
	}).Debug("Get certificate")

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.publicService.GetCert(
		ctx, &pb.GetCertRequest{Type: certType, Issuer: issuer, Serial: serial})
	if err != nil {
		return "", "", aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{
		"certURL": response.GetCertUrl(), "keyURL": response.GetKeyUrl(),
	}).Debug("Certificate info")

	return response.GetCertUrl(), response.GetKeyUrl(), nil
}

// SubscribeCertChanged subscribes client on CertChange events.
func (client *Client) SubscribeCertChanged(certType string) (<-chan *pb.CertInfo, error) {
	client.Lock()
	defer client.Unlock()

	ch := make(chan *pb.CertInfo, 1)

	if _, ok := client.certChangeSub[certType]; !ok {
		grpcStream, err := client.subscribeCertChange(certType)
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		subscription := &certChangeSub{
			grpcStream: &grpcStream,
			listeners:  []chan *pb.CertInfo{ch},
		}

		client.certChangeSub[certType] = subscription

		subscription.stopWG.Add(1)

		go client.processCertInfoChange(subscription)
	} else {
		subscription := client.certChangeSub[certType]
		subscription.listeners = append(subscription.listeners, ch)
	}

	return ch, nil
}

// UnsubscribeCertChanged unsubscribes client from CertChange event.
func (client *Client) UnsubscribeCertChanged(listener <-chan *pb.CertInfo) error {
	client.Lock()
	defer client.Unlock()

	for _, subscription := range client.certChangeSub {
		for ind, curListener := range subscription.listeners {
			if curListener == listener {
				subscription.listeners = append(subscription.listeners[:ind], subscription.listeners[ind+1:]...)

				return nil
			}
		}
	}

	return aoserrors.New("not found")
}

// GetCurrentNodeInfo returns current node info.
func (client *Client) GetCurrentNodeInfo() (cloudprotocol.NodeInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.publicService.GetNodeInfo(ctx, &empty.Empty{})
	if err != nil {
		return cloudprotocol.NodeInfo{}, aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{
		"nodeID":   response.GetNodeId(),
		"nodeType": response.GetNodeType(),
	}).Debug("Get current node Info")

	return pbconvert.NodeInfoFromPB(response), nil
}

// GetUnitSubjects returns unit subjects.
func (client *Client) GetUnitSubjects() (subjects []string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	request := &empty.Empty{}

	response, err := client.identService.GetSubjects(ctx, request)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{"subjects": response.GetSubjects()}).Debug("Get unit subjects")

	return response.GetSubjects(), nil
}

// SubscribeUnitSubjectsChanged subscribes client on unit subject changed events.
func (client *Client) SubscribeUnitSubjectsChanged() <-chan []string {
	client.Lock()
	defer client.Unlock()

	log.Debug("Subscribe on unit subjects change event")

	ch := make(chan []string, 1)
	client.subjectsSubs.listeners = append(client.subjectsSubs.listeners, ch)

	return ch
}

// RegisterInstance registers new service instance with permissions and create secret.
func (client *Client) RegisterInstance(
	instance aostypes.InstanceIdent, permissions map[string]map[string]string,
) (secret string, err error) {
	client.Lock()
	defer client.Unlock()

	log.WithFields(log.Fields{
		"serviceID": instance.ServiceID,
		"subjectID": instance.SubjectID,
		"instance":  instance.Instance,
	}).Debug("Register instance")

	if !client.isProtectedConnEnabled() {
		return "", errProtectedConnDisabled
	}

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	reqPermissions := make(map[string]*pb.Permissions)
	for key, value := range permissions {
		reqPermissions[key] = &pb.Permissions{Permissions: value}
	}

	response, err := client.permissionsService.RegisterInstance(ctx,
		&pb.RegisterInstanceRequest{Instance: pbconvert.InstanceIdentToPB(instance), Permissions: reqPermissions})
	if err != nil {
		return "", aoserrors.Wrap(err)
	}

	return response.GetSecret(), nil
}

// UnregisterInstance unregisters service instance.
func (client *Client) UnregisterInstance(instance aostypes.InstanceIdent) (err error) {
	client.Lock()
	defer client.Unlock()

	log.WithFields(log.Fields{
		"serviceID": instance.ServiceID,
		"subjectID": instance.SubjectID,
		"instance":  instance.Instance,
	}).Debug("Unregister instance")

	if !client.isProtectedConnEnabled() {
		return errProtectedConnDisabled
	}

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	if _, err := client.permissionsService.UnregisterInstance(ctx,
		&pb.UnregisterInstanceRequest{Instance: pbconvert.InstanceIdentToPB(instance)}); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// GetPermissions gets permissions by secret and functional server ID.
func (client *Client) GetPermissions(
	secret, funcServerID string,
) (instance aostypes.InstanceIdent, permissions map[string]string, err error) {
	client.Lock()
	defer client.Unlock()

	log.WithField("funcServerID", funcServerID).Debug("Get permissions")

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	req := &pb.PermissionsRequest{Secret: secret, FunctionalServerId: funcServerID}

	response, err := client.publicPermissionsService.GetPermissions(ctx, req)
	if err != nil {
		return instance, nil, aoserrors.Wrap(err)
	}

	return aostypes.InstanceIdent{
		ServiceID: response.GetInstance().GetServiceId(),
		SubjectID: response.GetInstance().GetSubjectId(), Instance: response.GetInstance().GetInstance(),
	}, response.GetPermissions().GetPermissions(), nil
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func (client *Client) openGRPCConnection() (err error) {
	log.Debug("Connecting to IAM...")

	var publicConn *grpc.ClientConn

	publicConn, err = grpchelpers.CreatePublicConnection(
		client.publicURL, client.cryptocontext, client.insecure)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if err := client.publicConnection.Start(publicConn); err != nil {
		return aoserrors.Wrap(err)
	}

	client.publicService = pb.NewIAMPublicServiceClient(client.publicConnection)
	client.identService = pb.NewIAMPublicIdentityServiceClient(client.publicConnection)
	client.publicNodesService = pb.NewIAMPublicNodesServiceClient(client.publicConnection)
	client.publicPermissionsService = pb.NewIAMPublicPermissionsServiceClient(client.publicConnection)

	if err = client.restoreCertInfoSubs(); err != nil {
		log.Error("Failed subscribe on CertInfo change")

		return aoserrors.Wrap(err)
	}

	if !client.isProtectedConnEnabled() {
		return nil
	}

	client.protectedConnection, err = grpchelpers.CreateProtectedConnection(client.certStorage,
		client.protectedURL, client.cryptocontext, client, client.insecure)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	client.certificateService = pb.NewIAMCertificateServiceClient(client.protectedConnection)
	client.provisioningService = pb.NewIAMProvisioningServiceClient(client.protectedConnection)
	client.nodesService = pb.NewIAMNodesServiceClient(client.protectedConnection)
	client.permissionsService = pb.NewIAMPermissionsServiceClient(client.protectedConnection)

	log.Debug("Connected to IAM")

	return nil
}

func (client *Client) closeGRPCConnection() {
	log.Debug("Closing IAM connection...")

	if client.publicConnection != nil {
		client.publicConnection.Stop()
	}

	if client.protectedConnection != nil {
		client.protectedConnection.Close()
	}

	for _, sub := range client.certChangeSub {
		sub.stopWG.Wait()
	}

	client.nodeInfoSubs.stopWG.Wait()
	client.subjectsSubs.stopWG.Wait()
}

func (client *Client) processCertChange(certChannel <-chan *pb.CertInfo) {
	for {
		select {
		case <-client.closeChannel:
			return

		case <-certChannel:
			client.onConnectionLost()
		}
	}
}

func (client *Client) subscribeNodeInfoChange() error {
	listener, err := client.publicNodesService.SubscribeNodeChanged(context.Background(), &emptypb.Empty{})
	if err != nil {
		log.WithField("error", err).Error("Can't subscribe on NodeChange event")

		return aoserrors.Wrap(err)
	}

	client.nodeInfoSubs.grpcStream = &listener

	client.nodeInfoSubs.stopWG.Add(1)
	go client.processNodeInfoChange(client.nodeInfoSubs)

	return nil
}

func (client *Client) subscribeUnitSubjectsChange() error {
	listener, err := client.identService.SubscribeSubjectsChanged(context.Background(), &emptypb.Empty{})
	if err != nil {
		log.WithField("error", err).Errorf("Can't subscribe on unit subjects change event: %v", err)

		return aoserrors.Wrap(err)
	}

	client.subjectsSubs.grpcStream = &listener

	client.subjectsSubs.stopWG.Add(1)
	go client.processUnitSubjectsChange(client.subjectsSubs)

	return nil
}

func (client *Client) subscribeCertChange(certType string) (
	listener pb.IAMPublicService_SubscribeCertChangedClient, err error,
) {
	listener, err = client.publicService.SubscribeCertChanged(context.Background(),
		&pb.SubscribeCertChangedRequest{Type: certType})
	if err != nil {
		log.WithField("error", err).Error("Can't subscribe on CertChange event")

		return nil, aoserrors.Wrap(err)
	}

	return listener, aoserrors.Wrap(err)
}

func (client *Client) processNodeInfoChange(sub *nodeInfoChangeSub) {
	defer sub.stopWG.Done()

	for {
		nodeInfo, err := (*sub.grpcStream).Recv()
		if err != nil {
			log.WithField("err", err).Warning("Process NodeInfo change failed")

			client.onConnectionLost()

			return
		}

		client.Lock()
		for _, listener := range sub.listeners {
			listener <- pbconvert.NodeInfoFromPB(nodeInfo)
		}
		client.Unlock()
	}
}

func (client *Client) processUnitSubjectsChange(sub *subjectsChangeSub) {
	defer sub.stopWG.Done()

	for {
		subjects, err := (*sub.grpcStream).Recv()
		if err != nil {
			log.WithField("err", err).Warning("Process UnitSubjects change failed")

			client.onConnectionLost()

			return
		}

		client.Lock()
		for _, listener := range sub.listeners {
			listener <- subjects.GetSubjects()
		}
		client.Unlock()
	}
}

func (client *Client) processCertInfoChange(sub *certChangeSub) {
	log.Debug("Start process CertInfo change")

	defer sub.stopWG.Done()

	for {
		cert, err := (*sub.grpcStream).Recv()
		if err != nil {
			log.WithField("err", err).Warning("Process CertInfo change failed")

			client.onConnectionLost()

			return
		}

		client.Lock()
		for _, listener := range sub.listeners {
			listener <- cert
		}
		client.Unlock()
	}
}

func (client *Client) reconnect() {
	defer client.reconnectWG.Done()

	log.Debug("Reconnecting to IAM server...")

	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-client.closeChannel:
			// don't recover reconnecting state if client is closing
			client.isReconnecting.Store(true)

			return

		case <-timer.C:
			client.closeGRPCConnection()

			if err := client.openGRPCConnection(); err != nil {
				log.WithField("err", err).Error("Reconnection to IAM failed")

				timer.Reset(iamReconnectInterval)

				continue
			}

			log.Debug("Successfully reconnected to IAM server")

			client.isReconnecting.Store(false)

			return
		}
	}
}

func (client *Client) getSystemID() (systemID string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	request := &empty.Empty{}

	response, err := client.identService.GetSystemInfo(ctx, request)
	if err != nil {
		return "", aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{"systemID": response.GetSystemId()}).Debug("Get system ID")

	return response.GetSystemId(), nil
}

func (client *Client) getCertTypes(nodeID string) ([]string, error) {
	log.WithField("nodeID", nodeID).Debug("Get certificate types")

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.provisioningService.GetCertTypes(
		ctx, &pb.GetCertTypesRequest{NodeId: nodeID})
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return response.GetTypes(), nil
}

func (client *Client) createKeys(nodeID, password string) (
	certs []cloudprotocol.IssueCertData, errorInfo *cloudprotocol.ErrorInfo,
) {
	certTypes, err := client.getCertTypes(nodeID)
	if err != nil {
		return nil, &cloudprotocol.ErrorInfo{Message: err.Error()}
	}

	for _, certType := range certTypes {
		log.WithFields(log.Fields{"nodeID": nodeID, "type": certType}).Debug("Create key")

		ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
		defer cancel()

		response, err := client.certificateService.CreateKey(ctx, &pb.CreateKeyRequest{
			Type:     certType,
			NodeId:   nodeID,
			Password: password,
		})
		if err != nil {
			return nil, &cloudprotocol.ErrorInfo{Message: err.Error()}
		}

		if response.GetError() != nil {
			return nil, pbconvert.ErrorInfoFromPB(response.GetError())
		}

		certs = append(certs, cloudprotocol.IssueCertData{
			Type:   certType,
			Csr:    response.GetCsr(),
			NodeID: nodeID,
		})
	}

	return certs, nil
}

func (client *Client) startProvisioning(nodeID, password string) (errorInfo *cloudprotocol.ErrorInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.provisioningService.StartProvisioning(
		ctx, &pb.StartProvisioningRequest{NodeId: nodeID, Password: password})
	if err != nil {
		return &cloudprotocol.ErrorInfo{Message: err.Error()}
	}

	return pbconvert.ErrorInfoFromPB(response.GetError())
}

func (client *Client) applyCertificates(
	nodeID string, certificates []cloudprotocol.IssuedCertData,
) (errorInfo *cloudprotocol.ErrorInfo) {
	for _, certificate := range certificates {
		log.WithFields(log.Fields{"nodeID": nodeID, "type": certificate.Type}).Debug("Apply certificate")

		ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
		defer cancel()

		response, err := client.certificateService.ApplyCert(
			ctx, &pb.ApplyCertRequest{
				NodeId: nodeID,
				Type:   certificate.Type,
				Cert:   certificate.CertificateChain,
			})
		if err != nil {
			return &cloudprotocol.ErrorInfo{Message: err.Error()}
		}

		if response.GetError() != nil {
			return pbconvert.ErrorInfoFromPB(response.GetError())
		}
	}

	return nil
}

func (client *Client) finishProvisioning(nodeID, password string) (errorInfo *cloudprotocol.ErrorInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.provisioningService.FinishProvisioning(
		ctx, &pb.FinishProvisioningRequest{NodeId: nodeID, Password: password})
	if err != nil {
		return &cloudprotocol.ErrorInfo{Message: err.Error()}
	}

	return pbconvert.ErrorInfoFromPB(response.GetError())
}

func (client *Client) restoreCertInfoSubs() error {
	for certType, sub := range client.certChangeSub {
		grpcStream, err := client.subscribeCertChange(certType)
		if err != nil {
			return err
		}

		sub.grpcStream = &grpcStream

		sub.stopWG.Add(1)

		go client.processCertInfoChange(sub)
	}

	return nil
}

func (client *Client) onConnectionLost() {
	select {
	case <-client.closeChannel:
		return

	default:
		if client.isReconnecting.CompareAndSwap(false, true) {
			client.reconnectWG.Add(1)
			go client.reconnect()
		}
	}
}

func (client *Client) isProtectedConnEnabled() bool {
	return len(client.protectedURL) > 0 && len(client.certStorage) > 0
}
