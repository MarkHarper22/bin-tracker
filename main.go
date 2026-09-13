package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

const (
	appName    = "Bin Tracker"
	appVersion = "1.0.0"
)

//go:embed web
var webFiles embed.FS

// App holds everything the HTTP handlers need.
type App struct {
	store      *Store
	dataDir    string
	httpPort   int
	httpsPort  int
	serverMode bool     // headless (Docker): no browser, no remote shutdown
	phoneURLs  []string // configured addresses for phones, if any
	quit       chan struct{}
}

// pauseOnFatal keeps a double-clicked console window open after an error.
var pauseOnFatal = true

func main() {
	port := flag.Int("port", envInt("BINTRACKER_PORT", 8420), "HTTP port; phones use HTTPS on port+1 [env BINTRACKER_PORT]")
	dataFlag := flag.String("data", os.Getenv("BINTRACKER_DATA"), "data folder (default: BinTracker-Data next to the app) [env BINTRACKER_DATA]")
	noBrowser := flag.Bool("no-browser", false, "don't open a browser window on start")
	server := flag.Bool("server", envBool("BINTRACKER_SERVER"), "run headless, e.g. in Docker: no browser, no Stop button, no prompts [env BINTRACKER_SERVER]")
	phoneFlag := flag.String("phone-url", os.Getenv("BINTRACKER_PHONE_URL"), "address phones should open, e.g. https://192.168.1.50:8421; comma-separate several [env BINTRACKER_PHONE_URL]")
	healthcheck := flag.Bool("healthcheck", false, "exit 0 if Bin Tracker answers on -port, otherwise 1 (Docker HEALTHCHECK)")
	flag.Parse()

	if *healthcheck {
		if alreadyRunning(*port) {
			os.Exit(0)
		}
		os.Exit(1)
	}
	if *server {
		pauseOnFatal = false
	}
	phoneURLs, err := parsePhoneURLs(*phoneFlag)
	if err != nil {
		fatal("%v", err)
	}

	localURL := fmt.Sprintf("http://localhost:%d", *port)

	// A second desktop launch just brings up the already-running copy.
	if !*server && alreadyRunning(*port) {
		fmt.Printf("%s is already running at %s\n", appName, localURL)
		if !*noBrowser {
			openBrowser(localURL)
		}
		return
	}

	dataDir, err := resolveDataDir(*dataFlag)
	if err != nil {
		fatal("Could not create a data folder: %v", err)
	}

	logFile, err := os.OpenFile(filepath.Join(dataDir, "bintracker.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
		defer logFile.Close()
	}

	store, err := OpenStore(filepath.Join(dataDir, "bintracker.db"), filepath.Join(dataDir, "backups"))
	if err != nil {
		fatal("Could not open the database: %v", err)
	}

	app := &App{
		store:      store,
		dataDir:    dataDir,
		httpPort:   *port,
		httpsPort:  *port + 1,
		serverMode: *server,
		phoneURLs:  phoneURLs,
		quit:       make(chan struct{}),
	}

	certFile, keyFile, err := ensureCert(dataDir, urlHosts(phoneURLs))
	if err != nil {
		fatal("Could not create the HTTPS certificate: %v", err)
	}

	handler := app.routes()
	httpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", app.httpPort))
	if err != nil {
		fatal("Port %d is already in use by another program. Start with -port <number> to pick another.\n(%v)", app.httpPort, err)
	}
	httpsLn, err := net.Listen("tcp", fmt.Sprintf(":%d", app.httpsPort))
	if err != nil {
		fatal("Port %d is already in use by another program. Start with -port <number> to pick another.\n(%v)", app.httpsPort, err)
	}

	httpSrv := &http.Server{Handler: handler, ReadHeaderTimeout: 15 * time.Second}
	httpsSrv := &http.Server{Handler: handler, ReadHeaderTimeout: 15 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	go func() {
		if err := httpSrv.Serve(httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server: %v", err)
		}
	}()
	go func() {
		if err := httpsSrv.ServeTLS(httpsLn, certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("https server: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go store.RunBackupScheduler(ctx)

	printBanner(app, localURL)
	if !*noBrowser && !app.serverMode {
		openBrowser(localURL)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case <-app.quit:
	}

	fmt.Println("\nShutting down...")
	cancel()
	shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = httpsSrv.Shutdown(shutdownCtx)
	store.Close()
	if app.serverMode {
		fmt.Println("Stopped.")
	} else {
		fmt.Println("Stopped. You can close this window.")
	}
}

func printBanner(app *App, localURL string) {
	fmt.Println()
	fmt.Println("==============================================================")
	if app.serverMode {
		fmt.Printf("  %s %s is running (server mode)\n", appName, appVersion)
		fmt.Println("==============================================================")
		fmt.Printf("  Web app:       HTTP on port %d\n", app.httpPort)
		fmt.Printf("  Phone cameras: HTTPS on port %d\n", app.httpsPort)
		phones := app.phoneAddresses()
		if len(phones) == 0 {
			fmt.Println("  Phone link:    shown in Settings when the web app is opened by the server's address")
		}
		for _, u := range phones {
			fmt.Printf("  Phone link:    %s\n", u)
		}
		fmt.Printf("  Data folder:   %s\n", app.dataDir)
	} else {
		fmt.Printf("  %s %s is running\n", appName, appVersion)
		fmt.Println("==============================================================")
		fmt.Printf("  On this computer:  %s\n", localURL)
		for _, u := range app.phoneAddresses() {
			fmt.Printf("  On phones (Wi-Fi): %s\n", u)
		}
		fmt.Printf("  Data folder:       %s\n", app.dataDir)
		fmt.Println()
		fmt.Println("  Leave this window open while using the app.")
		fmt.Println("  Close it (or press Ctrl+C) to stop Bin Tracker.")
	}
	fmt.Println("==============================================================")
	fmt.Println()
}

// phoneAddresses lists the links for connecting phones: configured URLs if
// set, otherwise this computer's network addresses. A container can't see
// the host's address, so in server mode the web page works it out instead.
func (a *App) phoneAddresses() []string {
	if len(a.phoneURLs) > 0 {
		return a.phoneURLs
	}
	out := []string{}
	if a.serverMode {
		return out
	}
	for _, ip := range lanIPs() {
		out = append(out, fmt.Sprintf("https://%s:%d", ip, a.httpsPort))
	}
	return out
}

// resolveDataDir picks a writable folder: the -data flag, then a folder next
// to the executable (portable), then the user's app-config folder.
func resolveDataDir(flagValue string) (string, error) {
	var candidates []string
	if flagValue != "" {
		candidates = append(candidates, flagValue)
	} else {
		if exe, err := os.Executable(); err == nil {
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			candidates = append(candidates, filepath.Join(filepath.Dir(exe), "BinTracker-Data"))
		}
		if cfg, err := os.UserConfigDir(); err == nil {
			candidates = append(candidates, filepath.Join(cfg, "BinTracker"))
		}
	}
	var lastErr error
	for _, dir := range candidates {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			lastErr = err
			continue
		}
		probe := filepath.Join(dir, ".write-test")
		if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
			lastErr = err
			continue
		}
		os.Remove(probe)
		abs, _ := filepath.Abs(dir)
		return abs, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no candidate folders")
	}
	return "", lastErr
}

func alreadyRunning(port int) bool {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ping", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		App string `json:"app"`
	}
	return json.NewDecoder(resp.Body).Decode(&body) == nil && body.App == "bintracker"
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// fatal shows the error and waits for Enter so a double-clicked console
// window doesn't vanish before the message can be read.
func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\nERROR: "+format+"\n", args...)
	if pauseOnFatal {
		fmt.Fprint(os.Stderr, "\nPress Enter to close.")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	os.Exit(1)
}
