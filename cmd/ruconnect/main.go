// Command ruconnect runs the RuConnect API server and web panel.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ruconnect/ruconnect/internal/api"
	"github.com/ruconnect/ruconnect/internal/config"
	"github.com/ruconnect/ruconnect/internal/store"
	"github.com/ruconnect/ruconnect/web"
)

const usage = `Usage:
  ruconnect                        run the server
  ruconnect healthcheck            exit 0 if the local server is healthy
  ruconnect passwd <user> <pass>   set a user's password (creates an admin if missing)
  ruconnect version                print the version

Configuration is read from RUCONNECT_* environment variables, see README.md.
`

func main() {
	cfg := config.Load()
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("RUCONNECT_LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "":
		err = serve(cfg, log)
	case "healthcheck":
		err = healthcheck(cfg)
	case "passwd":
		if len(os.Args) != 4 {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		err = passwd(cfg, os.Args[2], os.Args[3])
	case "version", "--version", "-v":
		fmt.Println(api.Version)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func serve(cfg config.Config, log *slog.Logger) error {
	st, err := store.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := seedAdmin(cfg, st, log); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := api.New(cfg, st, log, web.FS())
	go srv.RunMaintenance(ctx)

	hs := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("RuConnect started", "version", api.Version, "listen", cfg.Listen, "db", cfg.DB)
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// seedAdmin creates the first administrator on an empty database.
func seedAdmin(cfg config.Config, st *store.Store, log *slog.Logger) error {
	ctx := context.Background()
	n, err := st.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	pw := cfg.AdminPassword
	generated := pw == ""
	if generated {
		pw = store.RandomToken(8)
	}
	if _, err := st.CreateUser(ctx, store.UserInput{Name: cfg.AdminUser, Password: pw, Status: store.UserNormal, IsAdmin: true}); err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	if generated {
		log.Warn("created the first administrator; change this password in the panel",
			"user", cfg.AdminUser, "password", pw)
	} else {
		log.Info("created the first administrator", "user", cfg.AdminUser)
	}
	return nil
}

func healthcheck(cfg config.Config) error {
	addr := cfg.Listen
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: %s", resp.Status)
	}
	return nil
}

func passwd(cfg config.Config, name, pw string) error {
	if len(pw) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	st, err := store.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	u, err := st.UserByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		_, err = st.CreateUser(ctx, store.UserInput{Name: name, Password: pw, Status: store.UserNormal, IsAdmin: true})
		if err == nil {
			fmt.Printf("administrator %q created\n", name)
		}
		return err
	}
	if err != nil {
		return err
	}
	if err := st.SetPassword(ctx, u.ID, pw); err != nil {
		return err
	}
	fmt.Printf("password of %q changed\n", name)
	return nil
}
