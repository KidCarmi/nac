// Command nacd is the NAC control-plane daemon: the composition root that loads
// configuration, connects infrastructure, wires services and serves HTTP.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kidcarmi/nac/internal/app"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/observability"
	"github.com/kidcarmi/nac/pkg/version"
)

func main() {
	migrateOnly := flag.Bool("migrate", false, "apply database migrations and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	resetPassword := flag.String("reset-password", "", "set a new password for this operator email (read from stdin), revoke its sessions and exit")
	flag.Parse()
	if *showVersion {
		v := version.Get()
		fmt.Printf("nacd %s commit %s built %s\n", v.Version, v.Commit, v.BuildDate)
		return
	}

	if *resetPassword != "" {
		if err := resetOperatorPassword(*resetPassword); err != nil {
			fmt.Fprintln(os.Stderr, "nacd:", err)
			os.Exit(1)
		}
		fmt.Println("password changed; every session of", *resetPassword, "is signed out")
		return
	}

	if err := run(*migrateOnly); err != nil {
		fmt.Fprintln(os.Stderr, "nacd:", err)
		os.Exit(1)
	}
}

// resetOperatorPassword reads the new password from the first line of stdin
// (never from argv, which other local users can read).
func resetOperatorPassword(email string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	line, err := bufio.NewReader(io.LimitReader(os.Stdin, 4096)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read new password from stdin: %w", err)
	}
	pw := strings.TrimRight(line, "\r\n")
	if pw == "" {
		return errors.New("no password on stdin (use the nac-reset-password helper)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return app.ResetOperatorPassword(ctx, cfg, email, pw)
}

func run(migrateOnly bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := observability.NewLogger(cfg.Log.Level, cfg.Log.Format)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer a.Close()

	if migrateOnly {
		logger.Info("migrations applied; exiting")
		return nil
	}

	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           a.Handler(),
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}

	a.StartBackground(ctx)

	servers := []*http.Server{srv}
	errCh := make(chan error, 2)
	go func() {
		logger.Info("nacd listening (HTTP)", "addr", cfg.HTTP.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	if cfg.HTTP.HTTPSAddr != "" {
		tlsSrv := &http.Server{
			Addr:              cfg.HTTP.HTTPSAddr,
			Handler:           a.Handler(),
			ReadTimeout:       cfg.HTTP.ReadTimeout,
			WriteTimeout:      cfg.HTTP.WriteTimeout,
			IdleTimeout:       cfg.HTTP.IdleTimeout,
			ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
			TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		}
		servers = append(servers, tlsSrv)
		go func() {
			logger.Info("nacd listening (HTTPS)", "addr", cfg.HTTP.HTTPSAddr)
			if err := tlsSrv.ListenAndServeTLS(cfg.HTTP.TLSCertFile, cfg.HTTP.TLSKeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("https listener: %w", err)
			}
		}()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down", "timeout", cfg.HTTP.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	for _, s := range servers {
		if err := s.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
	}
	// Give background workers (CoA retries, probes) a moment to observe ctx.
	time.Sleep(100 * time.Millisecond)
	return nil
}
