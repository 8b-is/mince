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
	"time"

	"mince/internal/compression/mincer"
	"mince/internal/compression/router"
	"mince/internal/config"
	"mince/internal/config/vakedhcl"
	"mince/internal/experimental/ralph"
	"mince/internal/felix"
	"mince/internal/litany"
	"mince/internal/pq"
	"mince/internal/storage/ccr"
	"mince/internal/transport/mcp"
	"mince/internal/transport/proxy"
	"mince/internal/transport/wireagent"
	"mince/internal/transport/wrap"
)

const version = "0.2.0"

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
	case "felix":
		runFelix(os.Args[2:])
	case "ralph":
		runRalph(os.Args[2:])
	case "init":
		runInit(os.Args[2:])
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
  mince proxy    [--config config/mince.vkd.hcl] [--addr 127.0.0.1:8787] [--min-bytes 1024] [--protect-tail 2] [--compress-timeout 20ms] [--ccr-dir DIR]
  mince wrap     [--config config/mince.vkd.hcl] [--port 0] [--ccr-dir DIR] <command> [args...]
  mince mcp      serve [--ccr-dir DIR]
  mince compress [--kind auto|json|log|code] [--min-bytes N] [--ccr-dir DIR] [file|-]
  mince retrieve [--grep PATTERN] [--offset N] [--limit N] [--ccr-dir DIR] <ccr_id>
  mince stats    [--addr http://127.0.0.1:8787]
  mince felix eval '<source>'
  mince ralph run [--plan] [--verify] [--max-iter 64] [--every 8]
  mince init     [--path config/mince.vkd.hcl] [--force]
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
	compressTimeout := fs.Duration("compress-timeout", 20*time.Millisecond, "per-request compression budget")
	ccrDir := fs.String("ccr-dir", "", "CCR store directory")
	configPath := fs.String("config", "", "runtime and capability config file")
	anthropic := fs.String("anthropic-upstream", "https://api.anthropic.com", "Anthropic upstream")
	openai := fs.String("openai-upstream", "https://api.openai.com", "OpenAI upstream")
	_ = fs.Parse(args)
	loaded := loadConfig(*configPath)
	settings := config.DefaultProxySettings()
	if loaded != nil {
		settings = loaded.Proxy
	}
	if !flagWasSet(fs, "addr") {
		*addr = settings.Addr
	}
	if !flagWasSet(fs, "min-bytes") {
		*minBytes = settings.MinBytes
	}
	if !flagWasSet(fs, "protect-tail") {
		*protectTail = settings.ProtectTail
	}
	if !flagWasSet(fs, "compress-timeout") {
		*compressTimeout = settings.CompressTimeout
	}
	if !flagWasSet(fs, "ccr-dir") {
		*ccrDir = settings.CCRDir
	}
	if !flagWasSet(fs, "anthropic-upstream") {
		*anthropic = settings.AnthropicUpstream
	}
	if !flagWasSet(fs, "openai-upstream") {
		*openai = settings.OpenAIUpstream
	}

	store := openStore(*ccrDir)
	srv := proxy.New(proxy.Config{
		Addr:              *addr,
		AnthropicUpstream: *anthropic,
		OpenAIUpstream:    *openai,
		MinBytes:          *minBytes,
		ProtectTail:       *protectTail,
		CompressTimeout:   *compressTimeout,
		Store:             store,
		Logger:            logger(),
		WireAgents:        configuredWireAgents(loaded, store),
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
	storeDir := *ccrDir
	if storeDir == "" {
		storeDir = defaultCCRDir()
	}
	fmt.Fprintf(os.Stderr, "mince: proxy on http://%s (ccr %s)\n", l.Addr(), storeDir)
	if err := srv.Serve(l); err != nil {
		fmt.Fprintf(os.Stderr, "mince: serve: %v\n", err)
		os.Exit(1)
	}
}

func runWrap(args []string) {
	fs := flag.NewFlagSet("wrap", flag.ExitOnError)
	port := fs.Int("port", 0, "proxy port (0 = ephemeral)")
	ccrDir := fs.String("ccr-dir", "", "CCR store directory")
	configPath := fs.String("config", "", "runtime and capability config file")
	_ = fs.Parse(args)
	loaded := loadConfig(*configPath)
	settings := config.DefaultProxySettings()
	if loaded != nil {
		settings = loaded.Proxy
	}
	if !flagWasSet(fs, "ccr-dir") {
		*ccrDir = settings.CCRDir
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "mince wrap: no command given")
		os.Exit(2)
	}
	store := openStore(*ccrDir)
	proxyConfig := proxy.Config{
		AnthropicUpstream: settings.AnthropicUpstream,
		OpenAIUpstream:    settings.OpenAIUpstream,
		MinBytes:          settings.MinBytes,
		ProtectTail:       settings.ProtectTail,
		CompressTimeout:   settings.CompressTimeout,
		Store:             store,
		Logger:            logger(),
		WireAgents:        configuredWireAgents(loaded, store),
	}
	code, err := wrap.Run(rest[0], rest[1:], *port, store, logger(), proxyConfig)
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

func loadConfig(path string) *vakedhcl.Config {
	if path == "" {
		return nil
	}
	cfg, err := vakedhcl.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mince: config %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "mince: config %s version=%s capabilities=%d wire_agents=%d ralph=%d\n",
		path, cfg.Version, len(cfg.Capabilities), len(cfg.WireAgents), len(cfg.Ralph))
	return cfg
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func configuredWireAgents(cfg *vakedhcl.Config, store *ccr.Store) *wireagent.Pipeline {
	if cfg == nil || len(cfg.WireAgents) == 0 {
		return wireagent.NewPipeline(&wireagent.TierShuntAgent{}, &wireagent.InlineCCRAgent{Store: store})
	}
	enabled := func(kind string) bool {
		for _, agent := range cfg.WireAgents {
			if agent.Kind == kind && agent.Enabled {
				return true
			}
		}
		return false
	}
	var agents []wireagent.Agent
	if enabled("cache_guard") {
		agents = append(agents, &wireagent.CacheGuardAgent{Store: store})
	}
	if enabled("tier_shunt") {
		agents = append(agents, &wireagent.TierShuntAgent{})
	}
	if enabled("inline_ccr") {
		agents = append(agents, &wireagent.InlineCCRAgent{Store: store})
	}
	// panic_splice is response-side behavior; the current proxy pipeline only
	// runs on requests, so it is intentionally not installed here.
	return wireagent.NewPipeline(agents...)
}

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	path := fs.String("path", filepath.Join("config", "mince.vkd.hcl"), "config path")
	force := fs.Bool("force", false, "overwrite an existing config")
	_ = fs.Parse(args)
	if _, err := os.Stat(*path); err == nil && !*force {
		fmt.Fprintf(os.Stderr, "mince init: %s already exists (use --force)\n", *path)
		os.Exit(1)
	}
	if dir := filepath.Dir(*path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mince init: %v\n", err)
			os.Exit(1)
		}
	}
	if err := os.WriteFile(*path, []byte(vakedhcl.SampleConfig), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "mince init: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "mince: wrote %s\n", *path)
}

func runFelix(args []string) {
	if len(args) == 0 || args[0] != "eval" || len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mince felix eval '<source>'")
		os.Exit(2)
	}
	p, err := felix.Compile(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "mince felix: %v\n", err)
		os.Exit(1)
	}
	v, err := p.Call("main", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mince felix: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(v.String(p))
}

func runRalph(args []string) {
	if len(args) == 0 || args[0] != "run" {
		fmt.Fprintln(os.Stderr, "usage: mince ralph run [--plan] [--verify] [--max-iter N] [--every N]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("ralph run", flag.ExitOnError)
	plan := fs.Bool("plan", false, "print the replay plan")
	verify := fs.Bool("verify", false, "verify the final provenance chain")
	maxIter := fs.Int("max-iter", 64, "iterations")
	every := fs.Int("every", 8, "replay cadence")
	_ = fs.Parse(args[1:])
	eng := ralph.NewEngine(*every)
	for i := 0; i < *maxIter; i++ {
		a := eng.Current().Bandit.Select()
		reward := pq.New(0, 1)
		if i%2 == 0 {
			reward = pq.New(1, 1)
		}
		if _, err := eng.Record(ralph.Turn{Action: a, Reward: reward}); err != nil {
			fmt.Fprintf(os.Stderr, "mince ralph: %v\n", err)
			os.Exit(1)
		}
	}
	p := eng.Current()
	if *plan {
		fmt.Printf("ralph plan: max_iter=%d every=%d ring=%d policy=%s\n", *maxIter, *every, ralph.RingSize, p.Bandit.Best())
	}
	if *verify {
		if p.Provenance == nil {
			fmt.Fprintln(os.Stderr, "mince ralph: no provenance frame")
			os.Exit(1)
		}
		if err := litany.VerifyChain([]*litany.Frame{p.Provenance}, [32]byte{}); err != nil {
			fmt.Fprintf(os.Stderr, "mince ralph: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("ralph verify: ok")
	}
	fmt.Printf("ralph: version=%d score=%s best=%s\n", p.Version, p.Score.String(), p.Bandit.Best())
}
