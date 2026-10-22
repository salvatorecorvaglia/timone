package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"timone/internal/api"
	"timone/internal/config"
	"timone/internal/credentials"
	"timone/internal/integrations"
	"timone/internal/prometheus"
	"timone/internal/provider"
	"timone/internal/service"
	"timone/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "credential-exec" {
		if e := credentials.Run(context.Background(), os.Args[2:], os.Stdout); e != nil {
			_, _ = os.Stderr.WriteString("Credential plugin failed\n")
			os.Exit(1)
		}
		return
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	options, err := parseCLI(os.Args[1:], os.Stdout)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if options.version {
		fmt.Fprintln(os.Stdout, "Timone", web.Version)
		return
	}
	if e := run(log, options); e != nil {
		log.Error("Timone stopped", "error", e)
		os.Exit(1)
	}
}
func run(log *slog.Logger, options cliOptions) error {
	c, e := config.LoadWithOverrides(options.overrides)
	if e != nil {
		return e
	}
	var p provider.Provider
	var history service.HistorySource
	if c.Mode == "demo" {
		if os.Getenv("TIMONE_ENV") != "development" {
			return fmt.Errorf("demo mode is only available with TIMONE_ENV=development")
		}
		p = provider.NewDemo()
	} else {
		p, e = provider.NewCluster(c)
		if e != nil {
			return e
		}
		if c.Prometheus.URL != "" {
			client, err := prometheus.New(c.Prometheus)
			if err != nil {
				return err
			}
			history = client
		}
	}
	svc, err := service.NewWithDiagnosticPolicy(p, history, c.Diagnostics)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return fmt.Errorf("cannot listen: %w", err)
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if options.parentPipe {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			stop()
		}()
	}
	broker := api.NewBroker()
	runtime := api.NewRuntime(ctx, svc, c, web.Assets(), broker, log, func(next config.Config) (*service.Service, error) {
		if next.Context != svc.Provider.Name() {
			next.GitOps = integrations.DesiredOptions{}
		}
		p, err := provider.NewCluster(next)
		if err != nil {
			return nil, err
		}
		sameCluster := false
		original, hasOriginal := svc.Provider.(provider.ClusterIdentityProvider)
		selected, hasSelected := p.(provider.ClusterIdentityProvider)
		if hasOriginal && hasSelected {
			sameCluster = original.ClusterIdentity() == selected.ClusterIdentity()
		}
		// A kubeconfig context name can be rebound to another API endpoint.
		// Never reuse external endpoints based on the display name alone.
		if !sameCluster && next.GitOps != (integrations.DesiredOptions{}) {
			next.GitOps = integrations.DesiredOptions{}
			p, err = provider.NewCluster(next)
			if err != nil {
				return nil, err
			}
		}
		var selectedHistory service.HistorySource
		if sameCluster {
			selectedHistory = history
		}

		return service.NewWithDiagnosticPolicy(p, selectedHistory, next.Diagnostics)
	})
	defer runtime.Close()
	server := &http.Server{Addr: c.Listen, Handler: runtime, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}

	errCh := make(chan error, 1)
	go func() {
		log.Info("Timone starting", "address", listener.Addr().String(), "mode", c.Mode, "cluster", p.Name(), "namespace", p.Namespace(), "authenticated", c.Token != "" || c.TokenFile != "" || c.AuthMode == "proxy", "auth_mode", c.AuthMode)
		errCh <- server.Serve(listener)
	}()
	if !options.noBrowser && c.Mode == "local" {
		if address := browserURL(listener.Addr()); address != "" {
			go func() {
				if err := openBrowser(ctx, address); err != nil {
					log.Warn("Could not open browser; open the URL manually", "url", address)
				}
			}()
		}
	}
	select {
	case e := <-errCh:
		stop()
		return e
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		e := server.Shutdown(shutdown)
		if e != nil {
			_ = server.Close()
		}
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	}
}
