package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/steinyanaa/routeglass/internal/agent"
	"github.com/steinyanaa/routeglass/internal/buildinfo"
	"github.com/steinyanaa/routeglass/internal/server"
	"github.com/steinyanaa/routeglass/internal/updater"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var e error
	switch os.Args[1] {
	case "server":
		e = runServer(ctx, os.Args[2:])
	case "agent":
		e = runAgent(ctx, os.Args[2:])
	case "doctor":
		e = doctor(os.Args[2:])
	case "version":
		fmt.Printf("routeglass %s (%s, %s) %s/%s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date, runtime.GOOS, runtime.GOARCH)
	case "update":
		e = update(ctx, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if e != nil {
		slog.Error("command failed", "error", e)
		os.Exit(1)
	}
}
func usage() { fmt.Fprintln(os.Stderr, "usage: routeglass server|agent|doctor|version|update") }
func runServer(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("server", flag.ContinueOnError)
	p := f.String("config", "/etc/routeglass/server.json", "config file")
	if e := f.Parse(args); e != nil {
		return e
	}
	c, e := server.LoadConfig(*p)
	if e != nil && os.IsNotExist(e) && *p == "/etc/routeglass/server.json" {
		c = server.Config{Listen: "127.0.0.1:8765", DataDir: "./data", PublicOrigin: "http://127.0.0.1:8765", InsecureCookies: true}
		e = nil
	}
	if e != nil {
		return e
	}
	s, e := server.New(c)
	if e != nil {
		return e
	}
	defer s.Close()
	slog.Info("server starting", "listen", c.Listen)
	return s.ListenAndServe(ctx)
}
func runAgent(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("agent", flag.ContinueOnError)
	p := f.String("config", "/etc/routeglass-agent/agent.json", "config file")
	if e := f.Parse(args); e != nil {
		return e
	}
	c, e := agent.LoadConfig(*p)
	if e != nil {
		return e
	}
	a, e := agent.New(c, *p)
	if e != nil {
		return e
	}
	slog.Info("agent starting", "listen", c.Listen)
	return a.Run(ctx)
}
func doctor(args []string) error {
	f := flag.NewFlagSet("doctor", flag.ContinueOnError)
	p := f.String("config", "/etc/routeglass/server.json", "config file")
	if e := f.Parse(args); e != nil {
		return e
	}
	b, e := os.ReadFile(*p)
	if e != nil {
		return e
	}
	var raw map[string]any
	if e = json.Unmarshal(b, &raw); e != nil {
		return fmt.Errorf("config JSON: %w", e)
	}
	listen, _ := raw["listen"].(string)
	if listen == "" {
		return fmt.Errorf("listen is required")
	}
	ln, e := net.Listen("tcp", listen)
	state := "available"
	if e == nil {
		ln.Close()
	} else {
		client := http.Client{Timeout: 3 * time.Second}
		healthy := false
		for _, path := range []string{"/healthz", "/readyz"} {
			resp, he := client.Get("http://" + listen + path)
			if he == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					healthy = true
				}
			}
		}
		if !healthy {
			return fmt.Errorf("listen %s is occupied and RouteGlass is not healthy: %w", listen, e)
		}
		state = "occupied by healthy RouteGlass"
	}
	fmt.Printf("config: %s\nlisten: %s %s\ndata_dir: %s\n", *p, listen, state, filepath.Clean(fmt.Sprint(raw["data_dir"])))
	return nil
}
func update(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("update", flag.ContinueOnError)
	component := f.String("component", "server", "server or agent")
	manifest := f.String("manifest", "https://github.com/steinyanaa/routeglass/releases/latest/download/release.json", "release manifest URL")
	if e := f.Parse(args); e != nil {
		return e
	}
	return updater.Run(ctx, *manifest, *component)
}
