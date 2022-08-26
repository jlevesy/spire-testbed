package main

import (
	"context"
	"flag"
	"log"
	"net"

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
		spireAgentSock  string
		bindAddress     string
	)

	flag.StringVar(&allowedClientID, "allowed-client-id", "", "allowed client")
	flag.StringVar(&spireAgentSock, "spire-agent-sock", "", "spire-agent socket")
	flag.StringVar(&bindAddress, "bind-address", ":3333", "server bind address")

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

	srv := grpc.NewServer(
		grpc.Creds(
			grpccredentials.MTLSServerCredentials(
				source,
				source,
				tlsconfig.AuthorizeID(
					spiffeid.RequireFromString(allowedClientID),
				),
			),
		),
	)

	listener, err := net.Listen("tcp", bindAddress)
	if err != nil {
		log.Fatal("Can't bind to addr", bindAddress)
	}

	echo.RegisterEchoServer(
		srv,
		&echoService{},
	)

	log.Println("Server listening on 0.0.0.0:3333")
	if err := srv.Serve(listener); err != nil {
		log.Fatal("Serve returned an error: ", err)
	}
}

type echoService struct {
	echo.UnimplementedEchoServer
}

func (e *echoService) Echo(ctx context.Context, req *echo.EchoRequest) (*echo.EchoReply, error) {
	log.Println("Received a request", req.Payload)

	return &echo.EchoReply{Payload: req.Payload}, nil
}
