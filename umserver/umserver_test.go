package umserver_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_servicemanager/wsclient"

	"gitpct.epam.com/epmd-aepr/aos_updatemanager/config"
	"gitpct.epam.com/epmd-aepr/aos_updatemanager/umserver"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const serverURL = "wss://localhost:8088"

/*******************************************************************************
 * Types
 ******************************************************************************/

type testClient struct {
	wsClient       *wsclient.Client
	messageChannel chan []byte
}

type testUpdater struct {
	version uint64
	state   int
}

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	configJSON := `{
	"Cert": "../data/crt.pem",
	"Key":  "../data/key.pem"
}`

	var cfg config.Config

	decoder := json.NewDecoder(strings.NewReader(configJSON))
	// Parse config
	if err := decoder.Decode(&cfg); err != nil {
		log.Fatalf("Can't parse config: %s", err)
	}

	url, err := url.Parse(serverURL)
	if err != nil {
		log.Fatalf("Can't parse url: %s", err)
	}

	cfg.ServerURL = url.Host

	server, err := umserver.New(&cfg, &testUpdater{})
	if err != nil {
		log.Fatalf("Can't create ws server: %s", err)
	}
	defer server.Close()

	// There is raise condition: after new listen is not started yet
	// so we need this delay to wait for listen
	time.Sleep(time.Second)

	ret := m.Run()

	time.Sleep(time.Second)

	server.Close()

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetStatus(t *testing.T) {
	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umserver.StatusMessage

	if err := client.sendRequest(&umserver.GetStatusReq{
		MessageHeader: umserver.MessageHeader{Type: umserver.StatusType}}, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}

	if response.Status != umserver.SuccessStatus {
		t.Errorf("Wrong updater status: %s", response.Status)
	}
}

func TestSystemUpgrade(t *testing.T) {
	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umserver.StatusMessage

	if err := client.sendRequest(&umserver.UpgradeReq{
		MessageHeader: umserver.MessageHeader{Type: umserver.UpgradeType},
		ImageVersion:  3}, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}

	if response.Status != umserver.SuccessStatus {
		t.Errorf("Wrong updater status: %s", response.Status)
	}

	if response.Operation != umserver.UpgradeType {
		t.Errorf("Wrong operation: %s", response.Operation)
	}
}

func TestSystemRevert(t *testing.T) {
	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umserver.StatusMessage

	if err := client.sendRequest(&umserver.RevertReq{
		MessageHeader: umserver.MessageHeader{Type: umserver.RevertType},
		ImageVersion:  3}, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}

	if response.Status != umserver.SuccessStatus {
		t.Errorf("Wrong updater status: %s", response.Status)
	}

	if response.Operation != umserver.RevertType {
		t.Errorf("Wrong operation: %s", response.Operation)
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newTestClient(url string) (client *testClient, err error) {
	client = &testClient{messageChannel: make(chan []byte, 1)}

	if client.wsClient, err = wsclient.New("TestClient", client.messageHandler); err != nil {
		return nil, err
	}

	if err = client.wsClient.Connect(url); err != nil {
		return nil, err
	}

	return client, nil
}

func (client *testClient) close() {
	client.wsClient.Close()
}

func (client *testClient) messageHandler(message []byte) {
	client.messageChannel <- message
}

func (client *testClient) sendRequest(request, response interface{}, timeout time.Duration) (err error) {
	if err := client.wsClient.SendMessage(request); err != nil {
		return err
	}

	select {
	case <-time.After(timeout):
		return errors.New("wait response timeout")

	case message := <-client.messageChannel:
		var header umserver.MessageHeader

		if err = json.Unmarshal(message, &header); err != nil {
			return err
		}

		if header.Type != umserver.StatusType && header.Error != "" {
			return errors.New(header.Error)
		}

		if err = json.Unmarshal(message, response); err != nil {
			return err
		}
	}

	return nil
}

func (updater *testUpdater) GetVersion() (version uint64) {
	return updater.version
}

func (updater *testUpdater) GetOperationVersion() (version uint64) {
	return updater.version
}

func (updater *testUpdater) GetState() (state int) {
	return updater.state
}

func (updater *testUpdater) GetLastError() (err error) {
	return nil
}

func (updater *testUpdater) Upgrade(version uint64, filesInfo []umserver.UpgradeFileInfo) (err error) {
	updater.version = version
	return nil
}

func (updater *testUpdater) Revert(version uint64) (err error) {
	updater.version = version
	return nil
}
