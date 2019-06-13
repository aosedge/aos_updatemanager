package wsserver

import (
	"container/list"
	"context"
	"net/http"
	"sync"

	"gitpct.epam.com/epmd-aepr/aos_updatemanager/config"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// WsServer websocket server structure
type WsServer struct {
	httpServer *http.Server
	upgrader   websocket.Upgrader
	sync.Mutex
	clients *list.List
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new Web socket server
func New(config *config.Config) (server *WsServer, err error) {
	log.Debug("Create wsserver")

	server = &WsServer{}

	server.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", server.handleConnection)

	server.httpServer = &http.Server{Addr: config.ServerURL, Handler: serveMux}
	server.clients = list.New()

	go func(crt, key string) {
		log.WithFields(log.Fields{"address": config.ServerURL, "crt": crt, "key": key}).Debug("Listen for UM clients")

		if err := server.httpServer.ListenAndServeTLS(crt, key); err != http.ErrServerClosed {
			log.Error("Server listening error: ", err)
			return
		}
	}(config.Cert, config.Key)

	return server, nil
}

// Close closes web socket server and all connections
func (server *WsServer) Close() {
	log.Debug("Close server")

	server.Lock()
	defer server.Unlock()

	var next *list.Element
	for element := server.clients.Front(); element != nil; element = next {
		element.Value.(*wsClient).close()
		next = element.Next()
		server.clients.Remove(element)
	}

	server.httpServer.Shutdown(context.Background())
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (server *WsServer) handleConnection(w http.ResponseWriter, r *http.Request) {
	log.WithField("RemoteAddr", r.RemoteAddr).Debug("New connection request")

	if websocket.IsWebSocketUpgrade(r) != true {
		log.Error("New connection is not websocket")
		return
	}

	connection, err := server.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Can't make websocket connection: ", err)
		return
	}

	client, err := newClient(connection)
	if err != nil {
		log.Error("Can't create websocket client connection: ", err)
		connection.Close()
		return
	}

	server.Lock()
	clientElement := server.clients.PushBack(client)
	server.Unlock()

	client.run()

	server.Lock()
	defer server.Unlock()

	for element := server.clients.Front(); element != nil; element = element.Next() {
		if element == clientElement {
			client.close()
			server.clients.Remove(clientElement)
			break
		}
	}
}
