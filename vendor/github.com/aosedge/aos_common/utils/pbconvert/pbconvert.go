// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2022 Renesas Electronics Corporation.
// Copyright (C) 2022 EPAM Systems, Inc.
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

package pbconvert

import (
	"github.com/aosedge/aos_common/aostypes"
	"github.com/aosedge/aos_common/api/cloudprotocol"
	pbcommon "github.com/aosedge/aos_common/api/common"
	pbiam "github.com/aosedge/aos_common/api/iamanager"
	pbsm "github.com/aosedge/aos_common/api/servicemanager"
)

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// InstanceFilterFromPB converts InstanceFilter from protobuf to AOS type.
func InstanceFilterFromPB(filter *pbsm.InstanceFilter) cloudprotocol.InstanceFilter {
	aosFilter := cloudprotocol.InstanceFilter{}

	if filter.GetServiceId() != "" {
		aosFilter.ServiceID = &filter.ServiceId
	}

	if filter.GetSubjectId() != "" {
		aosFilter.SubjectID = &filter.SubjectId
	}

	if filter.GetInstance() != -1 {
		instance := (uint64)(filter.GetInstance())

		aosFilter.Instance = &instance
	}

	return aosFilter
}

// InstanceFilterToPB converts InstanceFilter from AOS type to protobuf.
func InstanceFilterToPB(filter cloudprotocol.InstanceFilter) *pbsm.InstanceFilter {
	ident := &pbsm.InstanceFilter{ServiceId: "", SubjectId: "", Instance: -1}

	if filter.ServiceID != nil {
		ident.ServiceId = *filter.ServiceID
	}

	if filter.SubjectID != nil {
		ident.SubjectId = *filter.SubjectID
	}

	if filter.Instance != nil {
		ident.Instance = (int64)(*filter.Instance)
	}

	return ident
}

// InstanceIdentFromPB converts InstanceIdent from protobuf to AOS type.
func InstanceIdentFromPB(ident *pbcommon.InstanceIdent) aostypes.InstanceIdent {
	return aostypes.InstanceIdent{
		ServiceID: ident.GetServiceId(),
		SubjectID: ident.GetSubjectId(),
		Instance:  ident.GetInstance(),
	}
}

// InstanceIdentToPB converts InstanceIdent from AOS type to protobuf.
func InstanceIdentToPB(ident aostypes.InstanceIdent) *pbcommon.InstanceIdent {
	return &pbcommon.InstanceIdent{ServiceId: ident.ServiceID, SubjectId: ident.SubjectID, Instance: ident.Instance}
}

// ErrorInfoFromPB converts ErrorInfo from protobuf to AOS type.
func ErrorInfoFromPB(errorInfo *pbcommon.ErrorInfo) *cloudprotocol.ErrorInfo {
	if errorInfo == nil {
		return nil
	}

	return &cloudprotocol.ErrorInfo{
		AosCode:  int(errorInfo.GetAosCode()),
		ExitCode: int(errorInfo.GetExitCode()),
		Message:  errorInfo.GetMessage(),
	}
}

// ErrorInfoToPB converts ErrorInfo from AOS type to protobuf.
func ErrorInfoToPB(errorInfo *cloudprotocol.ErrorInfo) *pbcommon.ErrorInfo {
	if errorInfo == nil {
		return nil
	}

	return &pbcommon.ErrorInfo{
		AosCode:  int32(errorInfo.AosCode),
		ExitCode: int32(errorInfo.ExitCode),
		Message:  errorInfo.Message,
	}
}

// NetworkParametersFromPB converts NetworkParameters from protobuf to AOS type.
func NewNetworkParametersFromPB(params *pbsm.NetworkParameters) aostypes.NetworkParameters {
	networkParams := aostypes.NetworkParameters{
		IP:            params.GetIp(),
		Subnet:        params.GetSubnet(),
		VlanID:        params.GetVlanId(),
		DNSServers:    make([]string, len(params.GetDnsServers())),
		FirewallRules: make([]aostypes.FirewallRule, len(params.GetRules())),
	}

	copy(networkParams.DNSServers, params.GetDnsServers())

	for i, rule := range params.GetRules() {
		networkParams.FirewallRules[i] = aostypes.FirewallRule{
			DstIP:   rule.GetDstIp(),
			SrcIP:   rule.GetSrcIp(),
			DstPort: rule.GetDstPort(),
			Proto:   rule.GetProto(),
		}
	}

	return networkParams
}

// NetworkParametersToPB converts NetworkParameters from AOS type to protobuf.
func NetworkParametersToPB(params aostypes.NetworkParameters) *pbsm.NetworkParameters {
	networkParams := &pbsm.NetworkParameters{
		Ip:         params.IP,
		Subnet:     params.Subnet,
		VlanId:     params.VlanID,
		DnsServers: make([]string, len(params.DNSServers)),
		Rules:      make([]*pbsm.FirewallRule, len(params.FirewallRules)),
	}

	copy(networkParams.GetDnsServers(), params.DNSServers)

	for i, rule := range params.FirewallRules {
		networkParams.Rules[i] = &pbsm.FirewallRule{
			DstIp:   rule.DstIP,
			SrcIp:   rule.SrcIP,
			DstPort: rule.DstPort,
			Proto:   rule.Proto,
		}
	}

	return networkParams
}

// NodeInfoFromPB converts NodeInfo from protobuf to Aos type.
func NodeInfoFromPB(pbNodeInfo *pbiam.NodeInfo) cloudprotocol.NodeInfo {
	nodeInfo := cloudprotocol.NodeInfo{
		NodeID:    pbNodeInfo.GetNodeId(),
		NodeType:  pbNodeInfo.GetNodeType(),
		Name:      pbNodeInfo.GetName(),
		Status:    pbNodeInfo.GetStatus(),
		OSType:    pbNodeInfo.GetOsType(),
		MaxDMIPs:  pbNodeInfo.GetMaxDmips(),
		TotalRAM:  pbNodeInfo.GetTotalRam(),
		ErrorInfo: ErrorInfoFromPB(pbNodeInfo.GetError()),
	}

	if pbNodeInfo.GetCpus() != nil {
		nodeInfo.CPUs = make([]cloudprotocol.CPUInfo, 0, len(pbNodeInfo.GetCpus()))

		for _, cpu := range pbNodeInfo.GetCpus() {
			nodeInfo.CPUs = append(nodeInfo.CPUs, cloudprotocol.CPUInfo{
				ModelName:  cpu.GetModelName(),
				NumCores:   cpu.GetNumCores(),
				NumThreads: cpu.GetNumThreads(),
				Arch:       cpu.GetArch(),
				ArchFamily: cpu.GetArchFamily(),
				MaxDMIPs:   cpu.GetMaxDmips(),
			})
		}
	}

	if pbNodeInfo.GetAttrs() != nil {
		nodeInfo.Attrs = make(map[string]interface{})

		for _, attr := range pbNodeInfo.GetAttrs() {
			nodeInfo.Attrs[attr.GetName()] = attr.GetValue()
		}
	}

	if pbNodeInfo.GetPartitions() != nil {
		nodeInfo.Partitions = make([]cloudprotocol.PartitionInfo, 0, len(pbNodeInfo.GetPartitions()))

		for _, partition := range pbNodeInfo.GetPartitions() {
			partitionInfo := cloudprotocol.PartitionInfo{
				Name:      partition.GetName(),
				Types:     partition.GetTypes(),
				TotalSize: partition.GetTotalSize(),
				Path:      partition.GetPath(),
			}

			nodeInfo.Partitions = append(nodeInfo.Partitions, partitionInfo)
		}
	}

	return nodeInfo
}
