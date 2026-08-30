package jot

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

//go:embed favicon.svg
var faviconSVG []byte

func (r *runner) serve(args []string) error {
	fs := newFlags("serve", r.errOut)
	// Bind to every interface by default: this is a personal wiki meant to be
	// read from a phone or laptop on the same trusted network.
	bind := fs.String("bind", "0.0.0.0", "interface to bind")
	port := fs.Int("port", 8787, "TCP port")
	vaultFlag := fs.String("vault", "", "vault path (defaults to configured vault)")
	watch := fs.Duration("watch", 0, "re-synchronize with GitHub on this interval, e.g. 5m")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return codedf(ExitUsage, "port must be between 1 and 65535")
	}
	root := *vaultFlag
	if root == "" {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		root = cfg.Vault
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root = abs
	if _, err := os.Stat(filepath.Join(root, "wiki")); err != nil {
		return fmt.Errorf("vault has no wiki directory: %w", err)
	}
	if err := syncLocked(r.ctx, root); err != nil {
		return err
	}

	addr := net.JoinHostPort(*bind, strconv.Itoa(*port))
	if *bind == "0.0.0.0" || *bind == "::" || *bind == "" {
		fmt.Fprintf(r.errOut, "WARNING: the compiled personal wiki is visible to every device that can reach port %d; raw captures are not served.\n", *port)
	}
	fmt.Fprintf(r.out, "Serving Jot wiki from %s on http://%s\n", root, addr)

	server := &http.Server{
		Addr:              addr,
		Handler:           newWikiHandler(root),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	if *watch > 0 {
		go r.watchLoop(root, *watch)
	}

	go func() {
		<-r.ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// syncLocked takes the vault lock for the duration of one synchronization.
func syncLocked(ctx context.Context, root string) error {
	lock, err := lockVault(root)
	if err != nil {
		return err
	}
	defer lock.Close()
	return syncBefore(ctx, root, false)
}

// watchLoop keeps a long-running server from drifting away from the remote.
func (r *runner) watchLoop(root string, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			if err := syncLocked(r.ctx, root); err != nil {
				fmt.Fprintf(r.errOut, "watch: %v\n", err)
			}
		}
	}
}

func newWikiHandler(root string) http.Handler {
	s := &wikiServer{root: root}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.home)
	mux.HandleFunc("GET /wiki/", s.page)
	mux.HandleFunc("GET /search", s.search)
	mux.HandleFunc("GET /api/search", s.apiSearch)
	mux.HandleFunc("GET /recent", s.recent)
	mux.HandleFunc("GET /loose-ends", s.looseEndsPage)
	mux.HandleFunc("GET /tags", s.tags)
	mux.HandleFunc("GET /tags/", s.tags)
	mux.HandleFunc("GET /graph", s.graph)
	mux.HandleFunc("GET /random", s.random)
	mux.HandleFunc("GET /log", s.log)
	mux.HandleFunc("GET /assets/wiki.css", s.css)
	mux.HandleFunc("GET /assets/wiki.js", s.js)
	mux.HandleFunc("GET /assets/favicon.svg", s.favicon)
	mux.HandleFunc("GET /healthz", s.health)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// connect-src is needed for the type-ahead endpoint; everything else
		// stays same-origin, and there is no inline script or style anywhere.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; "+
				"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, req)
	})
}

func (s *wikiServer) css(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(w, wikiCSS)
}

func (s *wikiServer) js(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = io.WriteString(w, wikiJS)
}

func (s *wikiServer) favicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(faviconSVG)
}

func (s *wikiServer) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok", "version": Version(),
	})
}
