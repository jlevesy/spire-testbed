package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"

	"github.com/sercand/kuberesolver/v3"
	"github.com/spiffe/go-spiffe/v2/spiffegrpc/grpccredentials"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"google.golang.org/grpc"

	"github.com/jlevesy/spire-testbed/pkg/echo"
)

func main() {
	var (
		ctx = context.Background()

		allowedClientID string
		allowedServerID string
		serverAddress   string
		spireAgentSock  string
		bindAddress     string
	)

	flag.StringVar(&allowedClientID, "allowed-client-id", "", "allowed client")
	flag.StringVar(&allowedServerID, "allowed-server-id", "", "allowed server")
	flag.StringVar(&serverAddress, "server-address", "", "server address")
	flag.StringVar(&spireAgentSock, "spire-agent-sock", "", "spire-agent socket")
	flag.StringVar(&bindAddress, "bind-address", ":8443", "server bind address")

	flag.Parse()

	source, err := workloadapi.NewX509Source(
		ctx,
		workloadapi.WithClientOptions(
			workloadapi.WithAddr(spireAgentSock),
		),
	)
	if err != nil {
		log.Fatalf("Unable to create X509Source: %v", err)
	}
	defer source.Close()

	kuberesolver.RegisterInCluster()

	conn, err := grpc.DialContext(
		ctx,
		serverAddress,
		grpc.WithTransportCredentials(
			grpccredentials.MTLSClientCredentials(
				source,
				source,
				tlsconfig.AuthorizeID(
					spiffeid.RequireFromString(allowedServerID),
				),
			),
		),
	)
	if err != nil {
		log.Fatalf("Unable to establish gRPC connection: %v", err)
	}

	defer conn.Close()

	var mux http.ServeMux

	mux.HandleFunc("/echo", echoHandler(echo.NewEchoClient(conn)))

	server := http.Server{
		Addr: bindAddress,
		TLSConfig: tlsconfig.MTLSServerConfig(
			source,
			source,
			tlsconfig.AuthorizeID(
				spiffeid.RequireFromString(allowedClientID),
			),
		),
		Handler: &mux,
	}

	log.Println("Serving on", serverAddress)

	if err := server.ListenAndServeTLS("", ""); err != nil {
		log.Fatalf("Error on serve: %v", err)
	}
}

type echoRequest struct {
	Message string `json:"message"`
}

type echoResponse struct {
	Message string `json:"message"`
}

func echoHandler(cl echo.EchoClient) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var req echoRequest

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Unexpected error while decoding body %v", err)
			http.Error(rw, "Bad payload", http.StatusBadRequest)
			return
		}

		resp, err := cl.Echo(r.Context(), &echo.EchoRequest{Payload: req.Message})
		if err != nil {
			log.Printf("Unable to call the server %v", err)
			http.Error(rw, "Soemthing went wrong", http.StatusInternalServerError)
			return
		}

		if err := json.NewEncoder(rw).Encode(&echoResponse{Message: resp.Payload}); err != nil {
			log.Printf("Unable to write response %v", err)
			http.Error(rw, "Soemthing went wrong", http.StatusInternalServerError)
			return
		}
	}
}
