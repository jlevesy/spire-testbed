package main

import (
	"context"
	"flag"
	"log"

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

		addr            string
		allowedServerID string
		spireAgentSock  string
	)

	flag.StringVar(&addr, "addr", "localhost:3333", "the address to connect to")
	flag.StringVar(&spireAgentSock, "spire-agent-sock", "", "spiffe-id to claim")
	flag.StringVar(&allowedServerID, "allowed-server-id", "", "allowed server")

	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("Must provide a message")
	}

	kuberesolver.RegisterInCluster()

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

	conn, err := grpc.DialContext(
		ctx,
		addr,
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
		log.Fatalf("Unable to dial: %v", err)
	}

	client := echo.NewEchoClient(conn)

	resp, err := client.Echo(ctx, &echo.EchoRequest{Payload: flag.Arg(0)})
	if err != nil {
		log.Fatal("unable to send echo request", err)
	}

	log.Println("Received echo message", resp.Payload)
}
