package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

type payload struct {
	Message string `json:"message"`
}

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

	var buf bytes.Buffer

	if err := json.NewEncoder(&buf).Encode(&payload{Message: flag.Arg(0)}); err != nil {
		log.Fatal("Can't encode", err)
	}

	req, err := http.NewRequest(http.MethodPost, addr, &buf)
	if err != nil {
		log.Fatal("LOL")
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsconfig.MTLSClientConfig(
				source,
				source,
				tlsconfig.AuthorizeID(
					spiffeid.RequireFromString(allowedServerID)),
			),
		},
	}

	r, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error connecting to %q: %v", addr, err)
	}

	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Fatalf("Unable to read body: %v", err)
	}

	log.Printf("%s", body)

}
