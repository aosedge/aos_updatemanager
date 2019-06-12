package httpsserver

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_vis/config"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// HTTPSServer https server structure
type HTTPSServer struct {
}

type status struct {
	Status string
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new https server
func New(config *config.Config) (server *HTTPSServer, err error) {
	log.Debug("Create https server")

	server = &HTTPSServer{}

	http.HandleFunc("/", server.handleProtocol)
	if err = http.ListenAndServeTLS(":443", "server.crt", "server.key", nil); err != nil {
		return nil, err
	}

	return server, nil
}

// Close closes web socket server and all connections
func (server *HTTPSServer) Close() {
	log.Debug("Close http server")
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (server *HTTPSServer) handleProtocol(w http.ResponseWriter, req *http.Request) {
	var status status

	w.Header().Set("Content-Type", "application/json")

	if req.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		status.Status = "Unsupported content type"
	}

	respJSON, err := json.Marshal(status)
	if err != nil {
		log.Errorf("Can't marshal JSON: %s", err)
	}

	w.Write(respJSON)
}
