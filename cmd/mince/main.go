// Command mince is a local context-compression proxy, agent wrapper and MCP
// server. Pure Go, no CGO, no third-party modules.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"mince/internal/ccr"
	"mince/internal/mcp"
	"mince/internal/mincer"
	"mince/internal/proxy"
	"mince/internal/router"
	"mince/internal/wrap"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "proxy":
		runProxy(os.Args[2:])
	case "wrap":
		runWrap(os.Args[2:])
	case "mcp":
		runMCP(os.Args[2:])
	case "compress":
		runCompress(os.Args[2:])
	case "retrieve":
		runRetrieve(os.Args[2:])
	case "stats":
		runStats(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("mince " + version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "mince: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mince — local reversible context compression

usage:
  mince proxy    [--addr 127.0.0.1:8787] [--min-bytes 1024] [--protect-tail 2] [--ccr-dir DIR]
  mince wrap     [--port 0] [--ccr-dir DIR] <command> [args...]
  mince mcp      serve [--ccr-dir DIR]
  mince compress [--kind auto|json|log|code] [--min-bytes N] [--ccr-dir DIR] [file|-]
  mince retrieve [--grep PATTERN] [--offset N] [--limit N] [--ccr-dir DIR] <ccr_id>
  mince stats    [--addr http://127.0.0.1:8787]
  mince version
`)
}

func defaultCCRDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "mince", "ccr")
}

func openStore(dir string) *ccr.Store {
	if dir == "" {
		dir = defaultCCRDir()
	}
	s, err := ccr.Open(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mince: ccr: %v\n", err)
		os.Exit(1)
	}
	return s
}

func logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func runProxy(args []string) {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")
	minBytes := fs.Int("min-bytes", 1024, "minimum content bytes to consider")
	protectTail := fs.Int("protect-tail", 2, "trailing turns never touched")
	ccrDir := fs.String("ccr-dir", "", "CCR store directory")
	anthropic := fs.String("anthropic-upstream", "https://api.anthropic.com", "Anthropic upstream")
	openai := fs.String("openai-upstream", "https://api.openai.com", "OpenAI upstream")
	_ = fs.Parse(args)

	store := openStore(*ccrDir)
	srv := proxy.New(proxy.Config{
		Addr:              *addr,
		AnthropicUpstream: *anthropic,
		OpenAIUpstream:    *openai,
		MinBytes:          *minBytes,
		ProtectTail:       *protectTail,
		Store:             store,
		Logger:            logger(),
	})
	l, err := srv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mince: listen: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()
	fmt.Fprintf(os.Stderr, "mince: proxy on http://%s (ccr %s)\n", l.Addr(), *ccrDir)
	if err := srv.Serve(l); err != nil {
		fmt.Fprintf(os.Stderr, "mince: serve: %v\n", err)
		os.Exit(1)
	}
}

func runWrap(args []string) {
	fs := flag.NewFlagSet("wrap", flag.ExitOnError)
	port := fs.Int("port", 0, "proxy port (0 = ephemeral)")
	ccrDir := fs.String("ccr-dir", "", "CCR store directory")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "mince wrap: no command given")
		os.Exit(2)
	}
	store := openStore(*ccrDir)
	code, err := wrap.Run(rest[0], rest[1:], *port, store, logger())
	if err != nil {
		fmt.Fprintf(os.Stderr, "mince wrap: %v\n", err)
	}
	os.Exit(code)
}

func runMCP(args []string) {
	if len(args) == 0 || args[0] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: mince mcp serve [--ccr-dir DIR]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("mcp serve", flag.ExitOnError)
	ccrDir := fs.String("ccr-dir", "", "CCR store directory")
	_ = fs.Parse(args[1:])
	store := openStore(*ccrDir)
	if err := mcp.New(store).Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "mince mcp: %v\n", err)
		os.Exit(1)
	}
}

func runCompress(args []string) {
	fs := flag.NewFlagSet("compress", flag.ExitOnError)
	kind := fs.String("kind", "auto", "auto|json|log|code")
	minBytes := fs.Int("min-bytes", 1024, "minimum bytes to consider")
	ccrDir := fs.String("ccr-dir", "", "CCR store directory")
	_ = fs.Parse(args)

	var content []byte
	var err error
	if fs.NArg() == 0 || fs.Arg(0) == "-" {
		content, err = io.ReadAll(os.Stdin)
	} else {
		content, err = os.ReadFile(fs.Arg(0))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "mince compress: %v\n", err)
		os.Exit(1)
	}

	store := openStore(*ccrDir)
	res := mincer.Mince(context.Background(), content, mincer.Options{
		Kind:     router.ParseKind(*kind),
		MinBytes: *minBytes,
		Store:    store,
	})
	if _, err := os.Stdout.Write(res.Minced); err != nil {
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "mince: kind=%s ccr=%s saved=%s (%s) raw=%d out=%d passthrough=%v\n",
		res.Kind, res.CCRID, res.Saved.String(), res.Saved.Percent(), res.RawBytes, res.MincedBytes, res.Passthrough)
}

func runRetrieve(args []string) {
	fs := flag.NewFlagSet("retrieve", flag.ExitOnError)
	grep := fs.String("grep", "", "regexp or literal substring filter")
	offset := fs.Int("offset", 0, "line offset")
	limit := fs.Int("limit", 0, "max lines (0 = all)")
	ccrDir := fs.String("ccr-dir", "", "CCR store directory")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: mince retrieve [flags] <ccr_id>")
		os.Exit(2)
	}
	id := fs.Arg(0)
	for len(id) > 4 && id[:4] == "ccr_" {
		id = id[4:]
	}
	store := openStore(*ccrDir)
	text, err := store.Retrieve(id, *grep, *offset, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mince retrieve: %v\n", err)
		os.Exit(1)
	}
	_, _ = io.WriteString(os.Stdout, text)
}

func runStats(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8787", "proxy base URL")
	_ = fs.Parse(args)
	resp, err := http.Get(*addr + "/_mince/stats")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mince stats: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(os.Stdout, resp.Body)
}
