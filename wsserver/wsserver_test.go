package wsserver_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"

	"gitpct.epam.com/epmd-aepr/aos_updatemanager/config"
	"gitpct.epam.com/epmd-aepr/aos_updatemanager/wsserver"
)

const serverURL = "localhost:8088"

type messageHeader struct {
	Type      string  `json:"type"`
	RequestID string  `json:"requestId"`
	Error     *string `json:"error"`
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

	cfg.ServerURL = serverURL

	server, err := wsserver.New(&cfg)
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

func closeConnection(c *websocket.Conn) {
	c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	c.Close()
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetStatus(t *testing.T) {
	u := url.URL{Scheme: "wss", Host: serverURL, Path: "/"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("Can't connect to ws server %s", err)
	}
	defer closeConnection(c)

	var response wsserver.StatusMessage

	if err := sendRequest(c, &wsserver.GetStatusReq{
		MessageHeader: wsserver.MessageHeader{Type: wsserver.StatusType}}, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}
}

func TestSystemUpgrade(t *testing.T) {
	u := url.URL{Scheme: "wss", Host: serverURL, Path: "/"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("Can't connect to ws server %s", err)
	}
	defer closeConnection(c)

	var response wsserver.StatusMessage

	if err := sendRequest(c, &wsserver.UpgradeReq{
		MessageHeader: wsserver.MessageHeader{Type: wsserver.UpgradeType},
		ImageVersion:  3}, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}
}

func TestSystemRevert(t *testing.T) {
	u := url.URL{Scheme: "wss", Host: serverURL, Path: "/"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("Can't connect to ws server %s", err)
	}
	defer closeConnection(c)

	var response wsserver.StatusMessage

	if err := sendRequest(c, &wsserver.RevertReq{
		MessageHeader: wsserver.MessageHeader{Type: wsserver.RevertType},
		ImageVersion:  3}, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func sendRequest(c *websocket.Conn, request, response interface{}, timeout time.Duration) (err error) {
	message, err := json.Marshal(request)
	if err != nil {
		return err
	}

	if err := c.WriteMessage(websocket.TextMessage, message); err != nil {
		return err
	}

	if err := c.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}

	messageType, message, err := c.ReadMessage()
	if err != nil {
		return err
	}

	if messageType != websocket.TextMessage {
		return err
	}

	var header wsserver.MessageHeader

	if err = json.Unmarshal(message, &header); err != nil {
		return err
	}

	if header.Type != wsserver.StatusType && header.Error != "" {
		return errors.New(header.Error)
	}

	if err = json.Unmarshal(message, response); err != nil {
		return err
	}

	return nil
}
