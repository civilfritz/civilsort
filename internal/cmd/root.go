package cmd

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/civilfritz/civilsort/internal/db"
	"github.com/civilfritz/civilsort/internal/handler"
	"github.com/civilfritz/civilsort/internal/hub"
	"github.com/spf13/cobra"
)

// shutdownTimeout bounds how long in-flight requests get to finish on SIGTERM.
const shutdownTimeout = 10 * time.Second

var (
	dbPath string
	addr   string

	templateFS embed.FS
	staticFS   embed.FS
)

func NewRootCmd(tmpls embed.FS, static embed.FS) *cobra.Command {
	templateFS = tmpls
	staticFS = static

	rootCmd := &cobra.Command{
		Use:   "civilsort",
		Short: "Ranked-choice voting server using the Schulze method",
		RunE:  runServer,
		// Runs after argument validation, so usage errors still print help
		// but runtime errors don't.
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.SilenceUsage = true
		},
	}

	// Persistent flag (shared by all commands)
	dbDefault := "civilsort.db"
	if v := os.Getenv("CIVILSORT_DB"); v != "" {
		dbDefault = v
	}
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", dbDefault, "SQLite database path (env: CIVILSORT_DB)")

	// Server-only flag
	addrDefault := ":8080"
	if v := os.Getenv("CIVILSORT_ADDR"); v != "" {
		addrDefault = v
	}
	rootCmd.Flags().StringVar(&addr, "addr", addrDefault, "listen address (env: CIVILSORT_ADDR)")

	// Add subcommands
	rootCmd.AddCommand(NewListCmd())

	return rootCmd
}

// securityHeadersMiddleware adds security headers to all responses
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func runServer(cmd *cobra.Command, args []string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	tmpls, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return fmt.Errorf("parsing templates: %w", err)
	}

	hubManager := hub.NewManager()
	h := handler.New(database, tmpls, hubManager)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Static files skip UserMiddleware: they don't need a user.
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("loading static files: %w", err)
	}
	root := http.NewServeMux()
	root.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))
	root.Handle("/", h.UserMiddleware(mux))

	srv := &http.Server{
		Addr:              addr,
		Handler:           securityHeadersMiddleware(http.NewCrossOriginProtection().Handler(root)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("Listening on %s", addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	log.Print("Shutting down")
	// Shutdown doesn't track hijacked WebSocket connections, so close them
	// first; browsers reconnect with backoff.
	hubManager.CloseAll()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	return nil
}

func Execute(tmpls embed.FS, static embed.FS) error {
	return NewRootCmd(tmpls, static).Execute()
}
