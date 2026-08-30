package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/graphql-go/graphql"
)

const (
	maxJSONBodyBytes         = 64 * 1024
	maxGraphQLBodyBytes      = 16 * 1024
	maxWebSocketBytes        = 4 * 1024
	defaultSSEInterval       = time.Second
	connectionReportInterval = 15 * time.Minute
	shutdownTimeout          = 5 * time.Second
	writeWait                = 10 * time.Second
	pongWait                 = 60 * time.Second
	pingPeriod               = (pongWait * 9) / 10
	correlationHeader        = "X-Testkit-Correlation-ID"
	correlationQuery         = "correlation_id"
	versionHeader            = "Testkit-Version"
)

//go:embed VERSION
var embeddedVersion string

// webFiles is embedded so the final scratch image needs no filesystem asset.
//
//go:embed templates/*.html templates/components/*.html static/*
var webFiles embed.FS

var (
	staticAssetVersion     = mustStaticAssetVersion(webFiles)
	indexPageTemplates     = newPageTemplates("index.html")
	webSocketPageTemplates = newPageTemplates("websocket.html")
	restPageTemplates      = newPageTemplates("rest.html")
	graphqlPageTemplates   = newPageTemplates("graphql-lab.html")
	ssePageTemplates       = newPageTemplates("sse.html")
	pageTranslationCatalog = mustLoadTranslationCatalog()
)

var version = strings.TrimSpace(embeddedVersion)

type response struct {
	Status         string `json:"status"`
	Version        string `json:"version"`
	UpstreamStatus int    `json:"upstreamStatus,omitempty"`
	UpstreamBody   string `json:"upstreamBody,omitempty"`
}

type item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type pageData struct {
	TitleKey           string
	Version            string
	StaticAssetVersion string
	Locale             string
	Texts              map[string]string
	TranslationsJSON   string
	Languages          []languageOption
	HomeURL            string
	LabEndpoint        string
	Protocols          []protocolCardView
	Clients            []webSocketClientView
	Breadcrumbs        []breadcrumb
}

func (data pageData) T(key string) string {
	if value, ok := data.Texts[key]; ok {
		return value
	}
	return key
}

type breadcrumb struct {
	Label   string
	URL     string
	Current bool
}

type languageOption struct {
	Code    string
	Label   string
	URL     string
	Current bool
}

type protocolDefinition struct {
	Index          string
	ID             string
	NameKey        string
	Endpoint       string
	DescriptionKey string
	URL            string
	StatusClass    string
	StatusKey      string
}

type protocolCardView struct {
	Index       string
	ID          string
	Name        string
	Endpoint    string
	Description string
	URL         string
	Active      bool
	OpenLab     string
	ComingSoon  string
	StatusBadge statusBadgeView
}

type statusBadgeView struct {
	Class string
	Label string
}

type webSocketClientView struct {
	ID             string
	Label          string
	Endpoint       string
	Texts          map[string]string
	DefaultMessage string
}

func (view webSocketClientView) T(key string) string {
	if value, ok := view.Texts[key]; ok {
		return value
	}
	return key
}

type handlerConfig struct {
	sseInterval time.Duration
	logger      *slog.Logger
	peers       *peerMonitor
	persistence *persistenceStore
}

type application struct {
	handler     http.Handler
	hub         *webSocketHub
	logger      *slog.Logger
	connections *connectionStats
	peers       *peerMonitor
}

const (
	protocolWebSocket = "websocket"
	protocolSSE       = "sse"
)

type connectionStats struct {
	mutex     sync.Mutex
	webSocket int
	sse       int
	sequence  uint64
}

func (stats *connectionStats) change(protocol string, delta int) (protocolActive, totalActive int, sequence uint64) {
	stats.mutex.Lock()
	defer stats.mutex.Unlock()
	stats.sequence++
	if protocol == protocolWebSocket {
		stats.webSocket += delta
		return stats.webSocket, stats.webSocket + stats.sse, stats.sequence
	}
	stats.sse += delta
	return stats.sse, stats.webSocket + stats.sse, stats.sequence
}

func (stats *connectionStats) snapshot() (webSocket, sse int, sequence uint64) {
	stats.mutex.Lock()
	defer stats.mutex.Unlock()
	return stats.webSocket, stats.sse, stats.sequence
}

func newPageTemplates(page string) *template.Template {
	return template.Must(
		template.New("base.html").
			Option("missingkey=error").
			ParseFS(webFiles, "templates/base.html", "templates/components/*.html", "templates/"+page),
	)
}

func mustStaticAssetVersion(files fs.FS) string {
	assetVersion, err := staticAssetVersionFromFS(files)
	if err != nil {
		panic(fmt.Sprintf("calculate static asset version: %v", err))
	}
	return assetVersion
}

func staticAssetVersionFromFS(files fs.FS) (string, error) {
	digest := sha256.New()
	err := fs.WalkDir(files, "static", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		_, _ = digest.Write([]byte(path))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(data)
		_, _ = digest.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)[:16]), nil
}

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] == "probe" {
			os.Exit(runProbe(context.Background(), os.Args[2:], os.Stdout, os.Stderr))
		}
		fmt.Fprintln(os.Stderr, "usage: testkit [probe URL]")
		os.Exit(2)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(
		"service", "molejo-testkit",
		"version", version,
	)
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	peerMonitor, err := loadPeerMonitorFromEnv(logger)
	if err != nil {
		logger.ErrorContext(ctx, "peer configuration failed", "event", "peers.configuration_failed", "error", err)
		os.Exit(1)
	}
	var persistence *persistenceStore
	if persistencePath := strings.TrimSpace(os.Getenv("TESTKIT_PERSISTENCE_FILE")); persistencePath != "" {
		persistence, err = newPersistenceStore(persistencePath)
		if err != nil {
			logger.ErrorContext(ctx, "persistence configuration failed", "event", "persistence.configuration_failed", "error", err)
			os.Exit(1)
		}
	}
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		logger.ErrorContext(ctx, "server listen failed", "event", "server.listen_failed", "error", err)
		os.Exit(1)
	}
	app := newApplication(handlerConfig{sseInterval: sseIntervalFromEnv(), logger: logger, peers: peerMonitor, persistence: persistence})
	logger.InfoContext(ctx, "server started", "event", "server.started", "listen_address", listener.Addr().String())
	if err := serve(ctx, listener, app); err != nil {
		logger.ErrorContext(ctx, "server failed", "event", "server.failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped", "event", "server.stopped")
}

func serve(ctx context.Context, listener net.Listener, app *application) error {
	server := &http.Server{
		Handler:           app.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       30 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	reporterCtx, stopReporter := context.WithCancel(ctx)
	reporterDone := make(chan struct{})
	go func() {
		defer close(reporterDone)
		ticker := time.NewTicker(connectionReportInterval)
		defer ticker.Stop()
		app.reportActiveConnections(reporterCtx, ticker.C)
	}()
	defer func() {
		stopReporter()
		<-reporterDone
	}()
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		if app.peers != nil {
			app.peers.run(monitorCtx)
		}
	}()
	defer func() {
		stopMonitor()
		<-monitorDone
	}()

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	select {
	case err := <-serveResult:
		app.close()
		closeCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		waitErr := app.waitForWebSockets(closeCtx)
		if errors.Is(err, http.ErrServerClosed) {
			if waitErr != nil {
				return fmt.Errorf("wait for WebSocket connections: %w", waitErr)
			}
			return nil
		}
		if waitErr != nil {
			return errors.Join(err, fmt.Errorf("wait for WebSocket connections: %w", waitErr))
		}
		return err
	case <-ctx.Done():
		app.close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		if err := app.waitForWebSockets(shutdownCtx); err != nil {
			return fmt.Errorf("wait for WebSocket connections: %w", err)
		}
		if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func newHandler() http.Handler {
	return newHandlerWithConfig(handlerConfig{sseInterval: sseIntervalFromEnv()})
}

func newHandlerWithConfig(config handlerConfig) http.Handler {
	return newApplication(config).handler
}

func withVersionHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(versionHeader, version)
		next.ServeHTTP(writer, request)
	})
}

func newApplication(config handlerConfig) *application {
	if config.sseInterval <= 0 {
		config.sseInterval = defaultSSEInterval
	}
	if config.logger == nil {
		config.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	webSocketHub := newWebSocketHub()
	connections := &connectionStats{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", exactGET("/", redirectToLocalized("/")))
	mux.HandleFunc("/websocket", exactGET("/websocket", redirectToLocalized("/websocket")))
	mux.HandleFunc("/rest", exactGET("/rest", redirectToLocalized("/rest")))
	mux.HandleFunc("/graphql-lab", exactGET("/graphql-lab", redirectToLocalized("/graphql-lab")))
	mux.HandleFunc("/sse", exactGET("/sse", redirectToLocalized("/sse")))
	for _, currentLocale := range supportedLocales {
		homePath := localizedPath(currentLocale, "/")
		webSocketPath := localizedPath(currentLocale, "/websocket")
		restPath := localizedPath(currentLocale, "/rest")
		graphqlPath := localizedPath(currentLocale, "/graphql-lab")
		ssePath := localizedPath(currentLocale, "/sse")
		mux.HandleFunc(homePath, exactGET(homePath, localizedIndex(currentLocale)))
		mux.HandleFunc(webSocketPath, exactGET(webSocketPath, localizedWebSocketIndex(currentLocale)))
		mux.HandleFunc(restPath, exactGET(restPath, localizedRESTIndex(currentLocale)))
		mux.HandleFunc(graphqlPath, exactGET(graphqlPath, localizedGraphQLIndex(currentLocale)))
		mux.HandleFunc(ssePath, exactGET(ssePath, localizedSSEIndex(currentLocale)))
	}
	mux.HandleFunc("/static/", staticAsset)
	mux.HandleFunc("/healthz", exactGET("/healthz", writeOK))
	mux.HandleFunc("/readyz", exactGET("/readyz", writeOK))
	mux.HandleFunc("/not-ready", exactGET("/not-ready", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusServiceUnavailable, response{Status: "not-ready", Version: version})
	}))
	mux.HandleFunc("/api/status", logHTTPRequest(config.logger, "/api/status", exactGET("/api/status", writeOK)))
	mux.HandleFunc("/api/items", logHTTPRequest(config.logger, "/api/items", exactGET("/api/items", listItems)))
	mux.HandleFunc("/api/echo", logHTTPRequest(config.logger, "/api/echo", apiEcho))
	if config.peers != nil {
		mux.HandleFunc(peerIdentityPath, exactGET(peerIdentityPath, config.peers.identityHandler))
		mux.HandleFunc("/api/peers", exactGET("/api/peers", config.peers.stateHandler))
	}
	if config.persistence != nil {
		mux.HandleFunc("/api/persistence", logHTTPRequest(config.logger, "/api/persistence", config.persistence.handler))
	}
	mux.HandleFunc("/graphql", graphQL)
	mux.HandleFunc("/events", exactGET("/events", func(writer http.ResponseWriter, request *http.Request) {
		events(writer, request, config.sseInterval, config.logger, connections)
	}))
	mux.HandleFunc("/ws", func(writer http.ResponseWriter, request *http.Request) {
		webSocket(writer, request, webSocketHub, config.logger, connections)
	})

	return &application{
		handler:     withVersionHeader(mux),
		hub:         webSocketHub,
		logger:      config.logger,
		connections: connections,
		peers:       config.peers,
	}
}

func (app *application) close() {
	app.hub.close()
}

func (app *application) waitForWebSockets(ctx context.Context) error {
	return app.hub.wait(ctx)
}

func (app *application) reportActiveConnections(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
			app.logActiveConnections(ctx)
		}
	}
}

func (app *application) logActiveConnections(ctx context.Context) {
	webSocketActive, sseActive, sequence := app.connections.snapshot()
	if webSocketActive+sseActive == 0 {
		return
	}
	app.logger.InfoContext(ctx, "active connections",
		"event", "connections.snapshot",
		"websocket_active", webSocketActive,
		"sse_active", sseActive,
		"total_active", webSocketActive+sseActive,
		"connection_sequence", sequence,
	)
}

type responseLogWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func (writer *responseLogWriter) WriteHeader(statusCode int) {
	if writer.statusCode == 0 {
		writer.statusCode = statusCode
	}
	writer.ResponseWriter.WriteHeader(statusCode)
}

func (writer *responseLogWriter) Write(data []byte) (int, error) {
	if writer.statusCode == 0 {
		writer.statusCode = http.StatusOK
	}
	written, err := writer.ResponseWriter.Write(data)
	writer.bytes += written
	return written, err
}

func logHTTPRequest(logger *slog.Logger, route string, handler http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		correlationID := resolveCorrelationID(request.Header.Get(correlationHeader))
		writer.Header().Set(correlationHeader, correlationID)
		logWriter := &responseLogWriter{ResponseWriter: writer}
		handler(logWriter, request)
		statusCode := logWriter.statusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		logger.InfoContext(request.Context(), "HTTP request completed",
			"event", "http.request.completed",
			"http_method", request.Method,
			"route", route,
			"status_code", statusCode,
			"duration_ms", durationMilliseconds(time.Since(started)),
			"response_bytes", logWriter.bytes,
			"correlation_id", correlationID,
		)
	}
}

func resolveCorrelationID(value string) string {
	if isUUIDv7(value) {
		return strings.ToLower(value)
	}
	return newUUIDv7()
}

func isUUIDv7(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	encoded := value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		return false
	}
	return decoded[6]>>4 == 7 && decoded[8]&0xc0 == 0x80
}

func newUUIDv7() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(fmt.Errorf("generate correlation ID: %w", err))
	}
	milliseconds := uint64(time.Now().UnixMilli())
	id[0] = byte(milliseconds >> 40)
	id[1] = byte(milliseconds >> 32)
	id[2] = byte(milliseconds >> 24)
	id[3] = byte(milliseconds >> 16)
	id[4] = byte(milliseconds >> 8)
	id[5] = byte(milliseconds)
	id[6] = id[6]&0x0f | 0x70
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

func exactGET(path string, handler http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != path {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(writer, request)
	}
}

func redirectToLocalized(page string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "private, no-store")
		writer.Header().Add("Vary", "Accept-Language")
		writer.Header().Add("Vary", "Cookie")
		http.Redirect(writer, request, localizedPath(pageTranslationCatalog.detect(request), page), http.StatusFound)
	}
}

func localizedIndex(currentLocale locale) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		data := localizedPageData(currentLocale, "/", pageData{
			TitleKey:  "console.label",
			Version:   version,
			Protocols: localizedProtocolCards(currentLocale),
		})
		renderLocalizedPage(writer, indexPageTemplates, currentLocale, data)
	}
}

func localizedRESTIndex(currentLocale locale) http.HandlerFunc {
	return localizedLabIndex(currentLocale, "/rest", "rest.lab_title", "/api/*", "protocol.rest.name", restPageTemplates)
}

func localizedGraphQLIndex(currentLocale locale) http.HandlerFunc {
	return localizedLabIndex(currentLocale, "/graphql-lab", "graphql.lab_title", "/graphql", "protocol.graphql.name", graphqlPageTemplates)
}

func localizedSSEIndex(currentLocale locale) http.HandlerFunc {
	return localizedLabIndex(currentLocale, "/sse", "sse.lab_title", "/events", "protocol.sse.name", ssePageTemplates)
}

func localizedLabIndex(currentLocale locale, page, titleKey, endpoint, nameKey string, templates *template.Template) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		texts := pageTranslationCatalog.translations(currentLocale)
		data := localizedPageData(currentLocale, page, pageData{
			TitleKey:    titleKey,
			Version:     version,
			LabEndpoint: endpoint,
			Breadcrumbs: []breadcrumb{
				{Label: texts["navigation.home"], URL: localizedPath(currentLocale, "/")},
				{Label: texts[nameKey], Current: true},
			},
		})
		renderLocalizedPage(writer, templates, currentLocale, data)
	}
}

func localizedWebSocketIndex(currentLocale locale) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		texts := pageTranslationCatalog.translations(currentLocale)
		data := localizedPageData(currentLocale, "/websocket", pageData{
			TitleKey: "websocket.lab_title",
			Version:  version,
			Clients: []webSocketClientView{
				{ID: "client-a", Label: texts["client.a"], Endpoint: "/ws", Texts: texts, DefaultMessage: strings.ReplaceAll(texts["websocket.default_message"], "{client}", texts["client.a"])},
				{ID: "client-b", Label: texts["client.b"], Endpoint: "/ws", Texts: texts, DefaultMessage: strings.ReplaceAll(texts["websocket.default_message"], "{client}", texts["client.b"])},
			},
			Breadcrumbs: []breadcrumb{
				{Label: texts["navigation.home"], URL: localizedPath(currentLocale, "/")},
				{Label: "WebSocket", Current: true},
			},
		})
		renderLocalizedPage(writer, webSocketPageTemplates, currentLocale, data)
	}
}

func localizedProtocolCards(currentLocale locale) []protocolCardView {
	texts := pageTranslationCatalog.translations(currentLocale)
	definitions := []protocolDefinition{
		{Index: "01", ID: "rest", NameKey: "protocol.rest.name", Endpoint: "/api/*", DescriptionKey: "protocol.rest.description", URL: localizedPath(currentLocale, "/rest"), StatusClass: "active", StatusKey: "status.active"},
		{Index: "02", ID: "graphql", NameKey: "protocol.graphql.name", Endpoint: "/graphql", DescriptionKey: "protocol.graphql.description", URL: localizedPath(currentLocale, "/graphql-lab"), StatusClass: "active", StatusKey: "status.active"},
		{Index: "03", ID: "sse", NameKey: "protocol.sse.name", Endpoint: "/events", DescriptionKey: "protocol.sse.description", URL: localizedPath(currentLocale, "/sse"), StatusClass: "active", StatusKey: "status.active"},
		{Index: "04", ID: "websocket", NameKey: "home.websocket_name", Endpoint: "/ws", DescriptionKey: "home.websocket_description", URL: localizedPath(currentLocale, "/websocket"), StatusClass: "active", StatusKey: "status.active"},
	}
	cards := make([]protocolCardView, 0, len(definitions))
	for _, definition := range definitions {
		cards = append(cards, protocolCardView{
			Index:       definition.Index,
			ID:          definition.ID,
			Name:        texts[definition.NameKey],
			Endpoint:    definition.Endpoint,
			Description: texts[definition.DescriptionKey],
			URL:         definition.URL,
			Active:      definition.StatusClass == "active",
			OpenLab:     texts["home.open_lab"],
			ComingSoon:  texts["home.coming_soon"],
			StatusBadge: statusBadgeView{Class: definition.StatusClass, Label: texts[definition.StatusKey]},
		})
	}
	return cards
}

func localizedPageData(currentLocale locale, page string, data pageData) pageData {
	data.Locale = string(currentLocale)
	data.StaticAssetVersion = staticAssetVersion
	data.Texts = pageTranslationCatalog.translations(currentLocale)
	data.TranslationsJSON = pageTranslationCatalog.translationsJSON(currentLocale)
	data.Languages = languageOptions(currentLocale, page)
	data.HomeURL = localizedPath(currentLocale, "/")
	return data
}

func languageOptions(currentLocale locale, page string) []languageOption {
	texts := pageTranslationCatalog.translations(currentLocale)
	labels := map[locale]string{
		localeEN:   texts["language.en"],
		localePtBR: texts["language.pt_br"],
		localeEsAR: texts["language.es_ar"],
	}
	options := make([]languageOption, 0, len(supportedLocales))
	for _, optionLocale := range supportedLocales {
		options = append(options, languageOption{
			Code:    string(optionLocale),
			Label:   labels[optionLocale],
			URL:     localizedPath(optionLocale, page),
			Current: optionLocale == currentLocale,
		})
	}
	return options
}

func localizedPath(currentLocale locale, page string) string {
	if page == "/" {
		return "/" + string(currentLocale) + "/"
	}
	return "/" + string(currentLocale) + page
}

func renderLocalizedPage(writer http.ResponseWriter, templates *template.Template, currentLocale locale, data pageData) {
	writer.Header().Set("Content-Language", string(currentLocale))
	http.SetCookie(writer, &http.Cookie{
		Name:     "testkit_locale",
		Value:    string(currentLocale),
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		SameSite: http.SameSiteLaxMode,
	})
	renderPage(writer, templates, data)
}

func renderPage(writer http.ResponseWriter, templates *template.Template, data pageData) {
	var page bytes.Buffer
	if err := templates.ExecuteTemplate(&page, "base", data); err != nil {
		slog.Error("render dashboard failed", "event", "page.render_failed", "error", err)
		http.Error(writer, "static page unavailable", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	_, _ = page.WriteTo(writer)
}

func staticAsset(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	assetPath := strings.TrimPrefix(request.URL.Path, "/static/")
	if assetPath == "" || strings.Contains(assetPath, "..") {
		http.NotFound(writer, request)
		return
	}
	immutable := false
	if requestedVersion, versionedPath, found := strings.Cut(assetPath, "/"); found && requestedVersion == staticAssetVersion {
		assetPath = versionedPath
		immutable = true
	}
	if assetPath == "" {
		http.NotFound(writer, request)
		return
	}
	data, err := webFiles.ReadFile("static/" + assetPath)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	if immutable {
		writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(writer, request, assetPath, time.Time{}, bytes.NewReader(data))
}

func writeOK(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, response{Status: "ok", Version: version})
}

func listItems(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, []item{
		{ID: "item-1", Name: "First item"},
		{ID: "item-2", Name: "Second item"},
	})
}

func apiEcho(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/api/echo" {
		http.NotFound(writer, request)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload json.RawMessage
	if err := decodeJSONBody(writer, request, maxJSONBodyBytes, &payload); err != nil {
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		Data   json.RawMessage `json:"data"`
		Method string          `json:"method"`
		Path   string          `json:"path"`
	}{Data: payload, Method: request.Method, Path: request.URL.Path})
}

func graphQL(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Query         string                 `json:"query"`
		Variables     map[string]interface{} `json:"variables"`
		OperationName string                 `json:"operationName"`
	}
	if err := decodeJSONBody(writer, request, maxGraphQLBodyBytes, &payload); err != nil {
		return
	}
	if strings.TrimSpace(payload.Query) == "" {
		http.Error(writer, "GraphQL query is required", http.StatusBadRequest)
		return
	}

	result := graphql.Do(graphql.Params{
		Schema:         newGraphQLSchema(),
		RequestString:  payload.Query,
		VariableValues: payload.Variables,
		OperationName:  payload.OperationName,
	})
	writeJSON(writer, http.StatusOK, result)
}

func newGraphQLSchema() graphql.Schema {
	query := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"status": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.String),
				Resolve: func(graphql.ResolveParams) (interface{}, error) { return "ok", nil },
			},
			"version": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.String),
				Resolve: func(graphql.ResolveParams) (interface{}, error) { return version, nil },
			},
			"echo": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
				Args: graphql.FieldConfigArgument{
					"message": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
				},
				Resolve: func(params graphql.ResolveParams) (interface{}, error) {
					message, ok := params.Args["message"].(string)
					if !ok {
						return nil, errors.New("message must be a string")
					}
					return message, nil
				},
			},
		},
	})
	schema, err := graphql.NewSchema(graphql.SchemaConfig{Query: query})
	if err != nil {
		panic(err)
	}
	return schema
}

func observeConnection(ctx context.Context, logger *slog.Logger, stats *connectionStats, protocol, route, correlationID string) func() {
	started := time.Now()
	protocolActive, totalActive, sequence := stats.change(protocol, 1)
	logger.InfoContext(ctx, "connection opened",
		"event", "connection.opened",
		"protocol", protocol,
		"route", route,
		"protocol_active", protocolActive,
		"total_active", totalActive,
		"connection_sequence", sequence,
		"correlation_id", correlationID,
	)
	return func() {
		protocolActive, totalActive, sequence := stats.change(protocol, -1)
		logger.InfoContext(ctx, "connection closed",
			"event", "connection.closed",
			"protocol", protocol,
			"route", route,
			"protocol_active", protocolActive,
			"total_active", totalActive,
			"connection_sequence", sequence,
			"duration_ms", durationMilliseconds(time.Since(started)),
			"correlation_id", correlationID,
		)
	}
}

func events(writer http.ResponseWriter, request *http.Request, interval time.Duration, logger *slog.Logger, stats *connectionStats) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	correlationID := resolveCorrelationID(request.URL.Query().Get(correlationQuery))
	connectionClosed := observeConnection(request.Context(), logger, stats, protocolSSE, "/events", correlationID)
	defer connectionClosed()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for sequence := 1; ; sequence++ {
		data, err := json.Marshal(response{Status: "ok", Version: version})
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(writer, "id: %d\nevent: status\ndata: %s\n\n", sequence, data); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

type wsMessage struct {
	Message string `json:"message"`
	Version string `json:"version"`
}

type wsClient struct {
	hub  *webSocketHub
	conn *websocket.Conn
	send chan []byte
}

type webSocketHub struct {
	mutex    sync.Mutex
	clients  map[*wsClient]struct{}
	closed   bool
	handlers sync.WaitGroup
}

func newWebSocketHub() *webSocketHub {
	return &webSocketHub{clients: make(map[*wsClient]struct{})}
}

func (hub *webSocketHub) register(client *wsClient) bool {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	if hub.closed {
		return false
	}
	hub.handlers.Add(1)
	hub.clients[client] = struct{}{}
	return true
}

func (hub *webSocketHub) handlerDone() {
	hub.handlers.Done()
}

func (hub *webSocketHub) wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		hub.handlers.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (hub *webSocketHub) unregister(client *wsClient) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	if _, ok := hub.clients[client]; ok {
		delete(hub.clients, client)
		close(client.send)
	}
}

func (hub *webSocketHub) send(message []byte) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	for client := range hub.clients {
		select {
		case client.send <- message:
		default:
			delete(hub.clients, client)
			close(client.send)
			_ = client.conn.Close()
		}
	}
}

func (hub *webSocketHub) close() {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	if hub.closed {
		return
	}
	hub.closed = true
	for client := range hub.clients {
		delete(hub.clients, client)
		close(client.send)
		_ = client.conn.Close()
	}
}

var webSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     sameWebSocketOrigin,
}

func sameWebSocketOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsedOrigin, err := url.Parse(origin)
	if err != nil || (parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https") {
		return false
	}
	return strings.EqualFold(parsedOrigin.Host, request.Host)
}

func webSocket(writer http.ResponseWriter, request *http.Request, hub *webSocketHub, logger *slog.Logger, stats *connectionStats) {
	connection, err := webSocketUpgrader.Upgrade(writer, request, http.Header{versionHeader: []string{version}})
	if err != nil {
		return
	}
	client := &wsClient{hub: hub, conn: connection, send: make(chan []byte, 16)}
	if !client.hub.register(client) {
		_ = connection.Close()
		return
	}
	defer client.hub.handlerDone()
	correlationID := resolveCorrelationID(request.URL.Query().Get(correlationQuery))
	connectionClosed := observeConnection(request.Context(), logger, stats, protocolWebSocket, "/ws", correlationID)
	defer func() {
		client.hub.unregister(client)
		_ = connection.Close()
		connectionClosed()
	}()

	go client.writePump()
	client.readPump()
}

func (client *wsClient) readPump() {
	client.conn.SetReadLimit(maxWebSocketBytes)
	_ = client.conn.SetReadDeadline(time.Now().Add(pongWait))
	client.conn.SetPongHandler(func(string) error {
		return client.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		var payload struct {
			Message string `json:"message"`
		}
		if err := client.conn.ReadJSON(&payload); err != nil {
			return
		}
		message, err := json.Marshal(wsMessage{Message: payload.Message, Version: version})
		if err != nil {
			return
		}
		client.hub.send(message)
	}
}

func (client *wsClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = client.conn.Close()
	}()
	for {
		select {
		case message, ok := <-client.send:
			_ = client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func decodeJSONBody(writer http.ResponseWriter, request *http.Request, limit int64, destination interface{}) error {
	request.Body = http.MaxBytesReader(writer, request.Body, limit)
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
			return err
		}
		http.Error(writer, "invalid JSON request", http.StatusBadRequest)
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			http.Error(writer, "request body must contain one JSON value", http.StatusBadRequest)
		} else {
			http.Error(writer, "invalid JSON request", http.StatusBadRequest)
		}
		return err
	}
	return nil
}

func sseIntervalFromEnv() time.Duration {
	interval, err := time.ParseDuration(os.Getenv("SSE_INTERVAL"))
	if err != nil || interval <= 0 {
		return defaultSSEInterval
	}
	return interval
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload interface{}) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		slog.Error("encode response failed", "event", "response.encode_failed", "error", err)
	}
}
