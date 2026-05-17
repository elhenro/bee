// Headless run path: parse flags, build engine, drive one turn, exit.
//
// Skill shims and CI smoke all exec through here. Pipes user message in
// from args or stdin; streams assistant text to stdout; prints session id
// to stderr on exit so callers can resume or inspect.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/llm/mockprov"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/apply_patch"
	"github.com/elhenro/bee/internal/tools/edit_diff"
	"github.com/elhenro/bee/internal/tools/find"
	"github.com/elhenro/bee/internal/tools/grep"
	"github.com/elhenro/bee/internal/tools/hashline_edit"
	"github.com/elhenro/bee/internal/tools/knowledge_search"
	"github.com/elhenro/bee/internal/tools/knowledge_write"
	"github.com/elhenro/bee/internal/tools/ls"
	"github.com/elhenro/bee/internal/tools/read"
	"github.com/elhenro/bee/internal/tools/shell"
	"github.com/elhenro/bee/internal/tools/usertool"
	"github.com/elhenro/bee/internal/tools/write"
)

// runHeadlessReal is the actual headless implementation. main.go calls
// it via the runHeadless alias.
func runHeadlessReal(args []string) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	headless := fs.Bool("headless", true, "run without TUI (default true)")
	model := fs.String("model", "", "override config default_model")
	providerName := fs.String("provider", "", "override config default_provider")
	sandboxScope := fs.String("sandbox", "", "override sandbox scope (read-only|workspace-write|danger-full-access)")
	skillName := fs.String("skill", "", "run a skill as the user message (prompt-kind only in Wave 2)")
	thinking := fs.String("thinking", "", "thinking level: auto|off|low|medium|high|max (default: from config)")
	effort := fs.String("effort", "", "alias for --thinking: auto|off|low|medium|high|max")
	cavemanLvl := fs.String("caveman", "", "force caveman level: off|lite|full|ultra (overrides profile, even on tiny)")
	jsonOut := fs.Bool("json", false, "emit NDJSON events to stdout instead of text")
	allowedTools := fs.String("allowed-tools", "", "comma-list of tool names to expose (default: all)")
	sessionID := fs.String("session", "", "use this session id instead of a fresh uuid")
	writeFilter := fs.String("write-path-re", "", "regex; writes restricted to paths matching this regex (default: no filter)")
	extraTools := fs.String("extra-tools", "", "comma-list of expert-mode tools to add to the manifest (e.g. apply_patch,hashline_edit). default: off")
	verbose := fs.Bool("verbose", false, "show full tool output (default: compact one-line preview)")
	bgLoop := fs.Bool("bg-loop", false, "persist after first turn: write status sidecar, poll inbox for follow-ups")
	yes := fs.Bool("yes", false, "auto-approve any dangerous shell command without prompting (still blocks hardline-refused commands)")
	yolo := fs.Bool("yolo", false, "alias for --yes: auto-approve dangerous commands")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	_ = headless // single-path for now
	if *verbose {
		_ = os.Setenv("BEE_VERBOSE", "1")
	}

	var writeRe *regexp.Regexp
	if *writeFilter != "" {
		re, err := regexp.Compile(*writeFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee run: bad --write-path-re: %v\n", err)
			os.Exit(2)
		}
		writeRe = re
	}

	var userMsg string
	if len(fs.Args()) > 0 {
		userMsg = strings.Join(fs.Args(), " ")
	} else if *skillName != "" {
		// skill body drives the turn; stdin would block on tty.
		userMsg = ""
	} else {
		var err error
		userMsg, err = resolveUserMessage(nil, os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee run: %v\n", err)
			os.Exit(2)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		// in test mode we synthesize a stub config so missing API keys
		// don't break the smoke test.
		if os.Getenv("BEE_TEST_PROVIDER") != "" {
			cfg = config.Defaults()
		} else {
			fmt.Fprintf(os.Stderr, "bee run: config: %v\n", err)
			os.Exit(1)
		}
	}
	// scripted provider needs deterministic Stream-call count: disable the
	// classifier (mode=auto fires a side-query), compaction, and memory
	// selection. tests script exactly the calls they expect.
	if os.Getenv("BEE_TEST_PROVIDER") == "scripted" {
		cfg.Mode = "edit"
		cfg.Compaction.Enabled = false
		cfg.Memory.Enabled = false
	}
	applyOverrides(&cfg, *model, *providerName, *sandboxScope)
	// --effort is an alias for --thinking. Global `bee --effort` (stripped
	// in main.go) lands in BEE_EFFORT and is consumed here as the lowest-
	// priority source; explicit subcommand flags override env.
	effortVal := *effort
	if effortVal == "" {
		effortVal = os.Getenv("BEE_EFFORT")
	}
	if *thinking == "" && effortVal != "" {
		*thinking = effortVal
	}
	if *thinking != "" {
		cfg.Thinking = string(llm.ParseThinking(*thinking))
	}
	cavemanArg := *cavemanLvl
	if cavemanArg == "" {
		cavemanArg = os.Getenv("BEE_CAVEMAN") // global --caveman flag
	}
	if cavemanArg != "" {
		// validate so a typo fails fast instead of silently falling back to
		// profile default in ApplyProfile.
		lvl, err := caveman.ParseLevel(cavemanArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee run: %v\n", err)
			os.Exit(2)
		}
		cfg.Caveman = string(lvl)
	}
	if *extraTools != "" {
		for _, p := range strings.Split(*extraTools, ",") {
			if p = strings.TrimSpace(p); p != "" {
				cfg.ExtraTools = append(cfg.ExtraTools, p)
			}
		}
	}

	prov, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee run: provider: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	app := buildHeadlessApprover(cfg, *yes || *yolo)
	var reg *tools.Registry
	if writeRe != nil {
		reg, err = buildToolsFilteredWithApprover(cwd, cfg, writeRe, prov, storeDir, app)
	} else {
		reg, err = buildToolsWithApprover(cwd, cfg, prov, storeDir, app)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee run: tools: %v\n", err)
		os.Exit(1)
	}
	if *allowedTools != "" {
		if reg, err = filterTools(reg, *allowedTools); err != nil {
			fmt.Fprintf(os.Stderr, "bee run: %v\n", err)
			os.Exit(2)
		}
	}

	ensureFirstRun()
	skillReg := skills.NewRegistry()
	_ = skillReg.Load(skills.BaseDir()) // best-effort

	if *skillName != "" {
		s, ok := skillReg.Get(*skillName)
		if !ok {
			fmt.Fprintf(os.Stderr, "bee run: unknown skill %q\n", *skillName)
			os.Exit(2)
		}
		// prompt-kind: prepend body to user message. exec/mcp/http defer to v0.2.
		if s.Kind == skills.KindPrompt && s.Body != "" {
			if userMsg == "" {
				userMsg = s.Body
			} else {
				userMsg = s.Body + "\n\nUser: " + userMsg
			}
		}
	}

	sessID := *sessionID
	if sessID == "" {
		sessID = uuid.NewString()
	}
	roll, err := session.Open(sessID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee run: session: %v\n", err)
		os.Exit(1)
	}
	defer roll.Close()

	memStore := newKnowledgeAdapter(prov, cfg)

	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Memory:   memStore,
		Sessions: roll,
		Cfg:      cfg,
		Cwd:      cwd,
		Stdout:   os.Stdout,
		Costs:    cost.New(),
	}
	if *jsonOut {
		eng.JSONEmitter = jsonmode.New(os.Stdout)
		// text deltas already routed through emitter; suppress raw writes.
		eng.Stdout = io.Discard
	}

	if *bgLoop {
		// long-lived bg path: signal-cancellable ctx, no timeout.
		bgCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runBgLoop(bgCtx, eng, sessID, userMsg); err != nil {
			fmt.Fprintf(os.Stderr, "bee run: bg-loop: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	res, err := eng.Run(ctx, userMsg)
	if err != nil {
		if *jsonOut {
			eng.JSONEmitter.Emit(jsonmode.Event{Type: "error", Message: err.Error()})
		} else {
			fmt.Fprintf(os.Stderr, "\nbee run: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
		os.Exit(1)
	}
	_ = res
	if !*jsonOut {
		// trailing newline so prompts don't merge with assistant output
		fmt.Fprintln(os.Stdout)
	}
	fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
}

// runBgLoop persists the engine across turns: write status sidecar on every
// boundary, run a turn, write awaiting status with the assistant's final
// text, then poll the inbox for follow-up messages. Exits on ctx cancel.
func runBgLoop(ctx context.Context, eng *loop.Engine, sessID, firstMsg string) error {
	base := bgreg.Status{
		SessionID: sessID,
		PID:       os.Getpid(),
		Task:      firstMsg,
		Model:     eng.Cfg.DefaultModel,
		Cwd:       eng.Cwd,
		StartedAt: time.Now().UTC(),
	}

	msg := firstMsg
	var cursor int64
	for {
		s := base
		s.State = bgreg.StateActive
		s.UpdatedAt = time.Now().UTC()
		_ = bgreg.Write(s)

		res, err := eng.Run(ctx, msg)
		if err != nil {
			if ctx.Err() != nil {
				s.State = bgreg.StateDone
				s.UpdatedAt = time.Now().UTC()
				_ = bgreg.Write(s)
				return nil
			}
			s.State = bgreg.StateFailed
			s.LastResponse = err.Error()
			s.UpdatedAt = time.Now().UTC()
			_ = bgreg.Write(s)
			return err
		}

		s = base
		s.State = bgreg.StateAwaiting
		s.LastResponse = res.FinalText
		s.UpdatedAt = time.Now().UTC()
		_ = bgreg.Write(s)

		next, newCursor, err := waitForInbox(ctx, sessID, cursor)
		if err != nil {
			return err
		}
		cursor = newCursor
		if ctx.Err() != nil {
			s.State = bgreg.StateDone
			s.UpdatedAt = time.Now().UTC()
			_ = bgreg.Write(s)
			return nil
		}
		msg = next
	}
}

// waitForInbox polls the inbox until a message arrives or ctx is cancelled.
// Returns the concatenated text of all new messages and the advanced cursor.
func waitForInbox(ctx context.Context, sessID string, cursor int64) (string, int64, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", cursor, nil
		case <-ticker.C:
			msgs, nc, err := bgreg.InboxDrain(sessID, cursor)
			if err != nil {
				return "", cursor, err
			}
			if len(msgs) > 0 {
				var b strings.Builder
				for i, m := range msgs {
					if i > 0 {
						b.WriteString("\n\n")
					}
					b.WriteString(m.Text)
				}
				return b.String(), nc, nil
			}
		}
	}
}

func resolveUserMessage(positional []string, stdin io.Reader) (string, error) {
	if len(positional) > 0 {
		return strings.Join(positional, " "), nil
	}
	// stdin fallback. limit read so a stuck pipe doesn't hang forever.
	buf := make([]byte, 1<<20)
	n, _ := io.ReadFull(stdin, buf)
	s := strings.TrimSpace(string(buf[:n]))
	if s == "" {
		return "", fmt.Errorf("no user message: pass as args or stdin")
	}
	return s, nil
}

func applyOverrides(cfg *config.Config, model, provName, sandboxScope string) {
	if model != "" {
		cfg.DefaultModel = model
	}
	if provName != "" {
		cfg.DefaultProvider = provName
	}
	if sandboxScope != "" {
		cfg.Sandbox.Scope = sandboxScope
	}
}

func buildProvider(cfg config.Config) (llm.Provider, error) {
	inner, err := buildProviderInner(cfg)
	if err != nil {
		return nil, err
	}
	// XML/text-mode wrap: active profile opts in via ToolFormat="xml". Useful
	// for small local models that ignore native tool_calls (llama3.1:8b,
	// gemma3, phi3). Default "" keeps native tool calls.
	if config.ActiveProfile(cfg).ToolFormat == "xml" {
		inner = llm.NewTextMode(inner, llm.TextModeOptions{})
	}
	// prewarm: local providers don't expose context_length on /v1/models, so
	// the loop's budget falls back to a useless 4*SystemPromptBudget. Fire a
	// best-effort /api/show probe in the background and stash the answer in
	// the context cache so contextBudget reflects reality from turn one.
	if config.IsLocalProvider(cfg.DefaultProvider) {
		if pc, ok := cfg.Providers[cfg.DefaultProvider]; ok && pc.BaseURL != "" {
			go func(baseURL, modelID string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if n, err := llm.ProbeOllamaContext(ctx, http.DefaultClient, baseURL, modelID); err == nil && n > 0 {
					llm.RememberContextLength(modelID, n)
				}
			}(pc.BaseURL, cfg.DefaultModel)
		}
	}
	return inner, nil
}

func buildProviderInner(cfg config.Config) (llm.Provider, error) {
	// test stub short-circuit: deterministic responses, no network.
	switch os.Getenv("BEE_TEST_PROVIDER") {
	case "stub":
		return newStubProvider(), nil
	case "scripted":
		path := os.Getenv("BEE_TEST_SCRIPT")
		if path == "" {
			return nil, fmt.Errorf("BEE_TEST_PROVIDER=scripted requires BEE_TEST_SCRIPT=<fixture path>")
		}
		f, err := mockprov.Load(path)
		if err != nil {
			return nil, err
		}
		return mockprov.NewScripted(f), nil
	}
	prov, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok {
		return nil, fmt.Errorf("provider %q not configured", cfg.DefaultProvider)
	}
	// route by wire_api: chat → openai-compat, gemini → native, responses →
	// chatgpt-subscription backend, anything else falls through as unsupported.
	switch prov.WireAPI {
	case "", "chat":
		return llm.NewOpenAICompat(llm.OpenAICompatConfig{
			Name:    cfg.DefaultProvider,
			BaseURL: prov.BaseURL,
			EnvKey:  prov.EnvKey,
		}), nil
	case "gemini":
		key := cfg.APIKey
		if key == "" && prov.EnvKey != "" {
			key = os.Getenv(prov.EnvKey)
		}
		return llm.NewGemini(llm.GeminiConfig{
			BaseURL: prov.BaseURL,
			APIKey:  key,
		}), nil
	case "responses":
		cgCfg := llm.ChatGPTConfig{
			Name:    cfg.DefaultProvider,
			BaseURL: prov.BaseURL,
		}
		if prov.OAuth != nil {
			cgCfg.ClientID = prov.OAuth.ClientID
			cgCfg.TokenEndpoint = prov.OAuth.TokenEndpoint
			cgCfg.AccountIDHeader = prov.OAuth.AccountIDHeader
		}
		return llm.NewChatGPT(cgCfg), nil
	case "anthropic-messages":
		return llm.NewClaude(llm.ClaudeConfig{
			Name:    cfg.DefaultProvider,
			BaseURL: prov.BaseURL,
			EnvKey:  prov.EnvKey,
		}), nil
	default:
		return nil, fmt.Errorf("wire_api %q not supported yet", prov.WireAPI)
	}
}

// filterTools narrows reg to the comma-separated list of tool names.
// Unknown names are an error so typos fail loudly. Empty list returns reg
// unchanged.
func filterTools(reg *tools.Registry, csv string) (*tools.Registry, error) {
	want := make(map[string]bool)
	for _, name := range strings.Split(csv, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		want[name] = true
	}
	if len(want) == 0 {
		return reg, nil
	}
	out := tools.NewRegistry()
	for name := range want {
		t, ok := reg.Get(name)
		if !ok {
			return nil, fmt.Errorf("--allowed-tools: unknown tool %q", name)
		}
		if err := out.Register(t); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func buildTools(cwd string, cfg config.Config, prov llm.Provider, storeDir string) (*tools.Registry, error) {
	return buildToolsWithApprover(cwd, cfg, prov, storeDir, nil)
}

// newShellTool returns a shell tool with optional approval gating and the
// shell-environment options from cfg. nil app = no gating (matches
// pre-approval behavior).
func newShellTool(app approval.Approver, cfg config.Config) tools.Tool {
	opts := shell.Options{
		UseUserRC: cfg.Shell.UseUserRC,
		Shell:     cfg.Shell.Shell,
		RCFile:    cfg.Shell.RCFile,
	}
	if !opts.UseUserRC && opts.Shell == "" && opts.RCFile == "" {
		if app == nil {
			return shell.New()
		}
		return shell.NewWithApprover(app)
	}
	return shell.NewWithOptions(app, opts)
}

// buildHeadlessApprover wires the dangerous-command approval gate for the
// headless CLI.
//
//	autoYes=true → Static{AllowOnce}: every flagged command runs without prompt
//	                (hardline patterns still refuse).
//	autoYes=false → Cache wrapping a stdin CLI prompt. Persistent grants come
//	                from cfg.Sandbox.CommandAllowlist; AllowAlways picks append
//	                to that list on disk via PersistAllowlistEntry.
func buildHeadlessApprover(cfg config.Config, autoYes bool) approval.Approver {
	if autoYes {
		return approval.Static{Verdict: approval.AllowOnce}
	}
	cli := approval.NewCLI(os.Stdin, os.Stderr)
	return approval.NewCache(cli, cfg.Sandbox.CommandAllowlist, PersistAllowlistEntry)
}

// buildToolsWithApprover is buildTools that wires app into the shell tool so
// safety.DetectDangerous matches consult the user before running. Pass nil to
// disable gating.
func buildToolsWithApprover(cwd string, cfg config.Config, prov llm.Provider, storeDir string, app approval.Approver) (*tools.Registry, error) {
	prof := config.ActiveProfile(cfg)
	r := tools.NewRegistry()
	all := []tools.Tool{
		newShellTool(app, cfg),
		read.NewWithLimits(prof.ReadDefaultLines, prof.ReadMaxLines),
		grep.NewWithMax(cwd, prof.GrepMaxMatches),
		find.New(cwd),
		ls.New(cwd),
		write.New(cwd),
		edit_diff.New(cwd),
		hashline_edit.New(),
	}
	// apply_patch dropped on tiny — small models mis-emit unified diffs.
	if !prof.SkipApplyPatch {
		all = append(all, apply_patch.New())
	}
	if cfg.Memory.Enabled && storeDir != "" {
		topK := cfg.Memory.TopK
		all = append(all,
			knowledge_search.New(prov, cfg.DefaultModel, storeDir, topK),
			knowledge_write.New(storeDir),
		)
	}
	all = appendUserTools(all, cfg.UserTools)
	for _, t := range all {
		if isDisabledTool(cfg.DisabledTools, t.Spec().Name) {
			continue
		}
		if err := r.Register(t); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// appendUserTools wraps each [[user_tools]] entry as a tool. Malformed
// entries (empty name/command) are silently skipped so a typo in config
// doesn't crash bootstrapping.
func appendUserTools(all []tools.Tool, ut []config.UserTool) []tools.Tool {
	for _, u := range ut {
		t, err := usertool.New(u.Name, u.Command, u.Description)
		if err != nil {
			continue
		}
		all = append(all, t)
	}
	return all
}

// isDisabledTool reports whether name appears in the disabled set.
func isDisabledTool(disabled []string, name string) bool {
	for _, d := range disabled {
		if d == name {
			return true
		}
	}
	return false
}

// buildToolsFiltered is buildTools with a path-regex constraint threaded into
// every mutation tool. Read-only tools are unaffected.
func buildToolsFiltered(cwd string, cfg config.Config, writeRe *regexp.Regexp, prov llm.Provider, storeDir string) (*tools.Registry, error) {
	return buildToolsFilteredWithApprover(cwd, cfg, writeRe, prov, storeDir, nil)
}

// buildToolsFilteredWithApprover combines buildToolsFiltered with the shell
// approval hook.
func buildToolsFilteredWithApprover(cwd string, cfg config.Config, writeRe *regexp.Regexp, prov llm.Provider, storeDir string, app approval.Approver) (*tools.Registry, error) {
	prof := config.ActiveProfile(cfg)
	r := tools.NewRegistry()
	all := []tools.Tool{
		newShellTool(app, cfg),
		read.NewWithLimits(prof.ReadDefaultLines, prof.ReadMaxLines),
		grep.NewWithMax(cwd, prof.GrepMaxMatches),
		find.New(cwd),
		ls.New(cwd),
		write.NewWithFilter(cwd, writeRe),
		edit_diff.NewWithFilter(cwd, writeRe),
		hashline_edit.NewWithFilter(writeRe),
	}
	if !prof.SkipApplyPatch {
		all = append(all, apply_patch.NewWithFilter(writeRe))
	}
	if cfg.Memory.Enabled && storeDir != "" {
		topK := cfg.Memory.TopK
		all = append(all,
			knowledge_search.New(prov, cfg.DefaultModel, storeDir, topK),
			knowledge_write.New(storeDir),
		)
	}
	all = appendUserTools(all, cfg.UserTools)
	for _, t := range all {
		if isDisabledTool(cfg.DisabledTools, t.Spec().Name) {
			continue
		}
		if err := r.Register(t); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// knowledgeAdapter satisfies loop.KnowledgeStore using the knowledge
// package. phase 1 of the query is deterministic; phase 2 fires a tiny
// side-LLM call only when phase 1 returns fewer than two candidates.
type knowledgeAdapter struct {
	prov    llm.Provider
	model   string
	dir     string
	enabled bool
	topK    int
}

func newKnowledgeAdapter(p llm.Provider, cfg config.Config) *knowledgeAdapter {
	dir, _ := knowledge.StoreDir()
	topK := cfg.Memory.TopK
	if topK <= 0 {
		topK = 3
	}
	return &knowledgeAdapter{
		prov:    p,
		model:   cfg.DefaultModel,
		dir:     dir,
		enabled: cfg.Memory.Enabled,
		topK:    topK,
	}
}

func (k *knowledgeAdapter) Query(ctx context.Context, query string, _ []string) ([]knowledge.Record, error) {
	if !k.enabled || k.dir == "" {
		return nil, nil
	}
	// missing dir is not fatal — first run has no records.
	if _, err := os.Stat(k.dir); err != nil {
		return nil, nil
	}
	files, err := knowledge.ListEntries(k.dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	// phase 1: deterministic scoring against the user query.
	recs, err := knowledge.Query(ctx, k.dir, query, k.topK, knowledge.Options{})
	if err != nil {
		return nil, err
	}
	if len(recs) >= 2 || k.prov == nil {
		return recs, nil
	}
	// phase 2: ask a small side-LLM for keyword tags and re-score.
	hints, herr := knowledge.ExtractTags(ctx, k.prov, k.model, query)
	if herr != nil || len(hints) == 0 {
		return recs, nil
	}
	return knowledge.Query(ctx, k.dir, query, k.topK, knowledge.Options{HintTags: hints})
}

