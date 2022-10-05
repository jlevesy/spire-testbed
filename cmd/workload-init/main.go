package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"

	"github.com/hashicorp/hcl"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	entryv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/server/entry/v1"
	"github.com/spiffe/spire-api-sdk/proto/spire/api/types"
	"github.com/spiffe/spire/cmd/spire-server/util"
	"github.com/spiffe/spire/pkg/common/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
)

type config struct {
	Registrations []registration `hcl:"registrations"`
}

type registration struct {
	ParentID  string   `hcl:"parent_id"`
	SpiffeID  string   `hcl:"spiffe_id"`
	Admin     bool     `hcl:"admin"`
	Selectors []string `hcl:"selectors"`

	parsedParentID  spiffeid.ID
	parsedSpiffeID  spiffeid.ID
	parsedSelectors []*types.Selector
}

func (c *registration) Validate() error {
	if len(c.Selectors) == 0 {
		return errors.New("at least one selector is expected")
	}

	var err error

	if c.parsedSelectors, err = parseSelectors(c.Selectors); err != nil {
		return fmt.Errorf("invalid selectors %w", err)
	}

	if c.parsedParentID, err = spiffeid.FromString(c.ParentID); err != nil {
		return fmt.Errorf("invalid parentID: %w", err)
	}

	if c.parsedSpiffeID, err = spiffeid.FromString(c.SpiffeID); err != nil {
		return fmt.Errorf("invalid spiffeID: %w", err)
	}

	return nil
}

func main() {
	var (
		socketPath        string
		configPath        string
		applyIfHostNameEq string
	)

	flag.StringVar(&socketPath, "socketPath", "", "Path to the admin API socket")
	flag.StringVar(&configPath, "configPath", "", "Path to the configuration file")
	flag.StringVar(&applyIfHostNameEq, "applyIfHostNameEq", "", "Only run if the container hostname is equal to the given value")

	flag.Parse()

	logger, err := log.NewLogger()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	hostname, err := os.Hostname()
	if err != nil {
		logger.WithError(err).Fatal("Can't get container hostname")
	}

	if applyIfHostNameEq == "" || applyIfHostNameEq == hostname {
		if err := apply(ctx, configPath, socketPath, logger); err != nil {
			logger.WithError(err).Fatal("Unable to apply changes")
		}
	}

	logger.Info("All done, happily waiting for termination")

	<-ctx.Done()

	logger.Info("Exiting")
}

func apply(ctx context.Context, configPath, socketPath string, logger *log.Logger) error {
	confBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("unable to read config file: %w", err)
	}

	var cfg config

	if err := hcl.Unmarshal(confBytes, &cfg); err != nil {
		return fmt.Errorf("unable to unmarshal config file: %w", err)
	}

	logger.Info("Connecting to the admin API")
	conn, err := grpc.DialContext(
		ctx,
		socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithReturnConnectionError(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", addr)
		}),
	)
	if err != nil {
		return fmt.Errorf("could not connect to the admin API: %w", err)
	}

	defer conn.Close()

	client := entryv1.NewEntryClient(conn)

	for i, registration := range cfg.Registrations {
		if err := registration.Validate(); err != nil {
			return fmt.Errorf("invalid registration at index %d: %w", i, err)
		}

		exists, err := entryExists(ctx, registration, client, logger)
		if err != nil {
			return fmt.Errorf("unable to check if an entry exists: %w", err)
		}

		if !exists {
			if err := createEntry(ctx, registration, client, logger); err != nil {
				return fmt.Errorf("unable to create an entry: %w", err)
			}
		}
	}

	return nil
}

func entryExists(ctx context.Context, reg registration, client entryv1.EntryClient, logger *log.Logger) (bool, error) {
	logger.
		WithField("spiffeID", reg.SpiffeID).
		WithField("parentID", reg.ParentID).
		WithField("selectors", reg.Selectors).
		Info("Checking if the entry exists")

	entries, err := client.ListEntries(
		ctx,
		&entryv1.ListEntriesRequest{
			Filter: &entryv1.ListEntriesRequest_Filter{
				BySelectors: &types.SelectorMatch{
					Selectors: reg.parsedSelectors,
					Match:     types.SelectorMatch_MATCH_EXACT,
				},
			},
		},
	)

	if err != nil {
		return false, err
	}

	for _, entry := range entries.Entries {
		if entry.SpiffeId.TrustDomain == reg.parsedSpiffeID.TrustDomain().String() &&
			entry.SpiffeId.Path == reg.parsedSpiffeID.Path() &&
			entry.ParentId.TrustDomain == reg.parsedParentID.TrustDomain().String() &&
			entry.ParentId.Path == reg.parsedParentID.Path() {
			return true, nil
		}

	}

	return false, nil
}

func createEntry(ctx context.Context, reg registration, client entryv1.EntryClient, logger *log.Logger) error {
	logger.
		WithField("spiffeID", reg.SpiffeID).
		WithField("parentID", reg.ParentID).
		WithField("selectors", reg.Selectors).
		Info("Creating entry")

	resp, err := client.BatchCreateEntry(
		ctx,
		&entryv1.BatchCreateEntryRequest{
			Entries: []*types.Entry{
				{
					SpiffeId: &types.SPIFFEID{
						TrustDomain: reg.parsedSpiffeID.TrustDomain().String(),
						Path:        reg.parsedSpiffeID.Path(),
					},
					ParentId: &types.SPIFFEID{
						TrustDomain: reg.parsedParentID.TrustDomain().String(),
						Path:        reg.parsedParentID.Path(),
					},
					Selectors: reg.parsedSelectors,
					Admin:     reg.Admin,
				},
			},
		},
	)

	if err != nil {
		return err
	}

	var msgs []string

	for _, res := range resp.Results {
		if res.Status.Code == int32(codes.OK) {
			continue
		}

		msgs = append(msgs, res.Status.Message)
	}

	if len(msgs) != 0 {
		return errors.New(strings.Join(msgs, ","))
	}

	return nil
}

func parseSelectors(raw []string) ([]*types.Selector, error) {
	selectors := make([]*types.Selector, len(raw))
	for i, s := range raw {
		cs, err := util.ParseSelector(s)
		if err != nil {
			return nil, err
		}

		selectors[i] = cs
	}

	return selectors, nil
}
