// Command demo-server is a throwaway HTTP backend used to record the sinq
// showcase: an in-memory shop API with an auth flow, a handful of business
// domains, and randomized latency to imitate a real network.
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	mrand "math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// webhookSecret signs checkout payloads; demo/users/orders/checkout/.scenario holds the same value.
const webhookSecret = "demo-webhook-secret"

// signedJWT builds an HS256 token by hand, so the demo needs no dependencies.
func signedJWT(claims map[string]any) string {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	unsigned := header + "." + enc.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte("demo-session-secret"))
	mac.Write([]byte(unsigned))
	return unsigned + "." + enc.EncodeToString(mac.Sum(nil))
}

func randDuration(minMs, maxMs int) time.Duration {
	return time.Duration(minMs+mrand.IntN(maxMs-minMs+1)) * time.Millisecond
}

func latency(minMs, maxMs int) {
	time.Sleep(randDuration(minMs, maxMs))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func randomToken() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

var idCounters sync.Map // string(prefix) -> *atomic.Int64

func newID(prefix string) string {
	v, _ := idCounters.LoadOrStore(prefix, new(atomic.Int64))
	counter := v.(*atomic.Int64)
	return fmt.Sprintf("%s_%d", prefix, counter.Add(1))
}

type server struct {
	mu sync.Mutex

	validTokens map[string]bool
	users       map[string]map[string]any
	inventory   map[string]int
	shipmentAt  map[string]int
	notifAt     map[string]int
	searchAt    map[string]int
	reportAt    map[string]int
}

func newServer() *server {
	return &server{
		validTokens: map[string]bool{},
		users:       map[string]map[string]any{},
		inventory: map[string]int{
			"health_potion": 50,
			"steel_sword":   20,
			"wooden_shield": 35,
		},
		shipmentAt: map[string]int{},
		notifAt:    map[string]int{},
		searchAt:   map[string]int{},
		reportAt:   map[string]int{},
	}
}

func (s *server) issueToken() string {
	token := randomToken()
	s.mu.Lock()
	s.validTokens[token] = true
	s.mu.Unlock()
	return token
}

func (s *server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
			return
		}
		s.mu.Lock()
		valid := s.validTokens[token]
		s.mu.Unlock()
		if !valid {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		next(w, r)
	}
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	latency(200, 400)
	var body struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": s.issueToken()})
}

func (s *server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	latency(150, 400)
	token := signedJWT(map[string]any{
		"sub": "demo_user",
		"exp": time.Now().Add(15 * time.Minute).Unix(),
	})
	s.mu.Lock()
	s.validTokens[token] = true
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	latency(80, 220)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": "2.4.1"})
}

func (s *server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	id := newID("usr")
	user := map[string]any{"id": id, "name": body["name"], "email": body["email"]}
	s.mu.Lock()
	s.users[id] = user
	s.mu.Unlock()
	w.Header().Set("Location", "/users/"+id)
	writeJSON(w, http.StatusCreated, user)
}

func (s *server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	s.mu.Lock()
	user, ok := s.users[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	var patch map[string]any
	if err := readJSON(r, &patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	s.mu.Lock()
	user, ok := s.users[r.PathValue("id")]
	if ok {
		maps.Copy(user, patch)
	}
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleWishlist(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	var body struct {
		Products []string `json:"products"`
	}
	if err := readJSON(r, &body); err != nil || len(body.Products) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "products must be a non-empty array"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected multipart/form-data"})
		return
	}
	file, header, err := r.FormFile("avatar")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing avatar part"})
		return
	}
	defer file.Close()
	size, _ := io.Copy(io.Discard, file)
	writeJSON(w, http.StatusOK, map[string]any{
		"filename":     header.Filename,
		"content_type": header.Header.Get("Content-Type"),
		"size":         size,
	})
}

var catalog = []string{
	"health_potion", "steel_sword", "wooden_shield", "hunting_bow",
	"arcane_staff", "leather_armor", "silver_ring", "ancient_amulet",
	"traveler_boots", "shadow_cloak",
}

func (s *server) handleProducts(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	query := strings.ToLower(r.URL.Query().Get("query"))
	items := make([]string, 0, len(catalog))
	for _, item := range catalog {
		if query == "" || strings.Contains(item, query) {
			items = append(items, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) handleFeatured(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	writeJSON(w, http.StatusOK, map[string]any{"items": catalog[:4]})
}

func (s *server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	id := newID("ord")
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "pending"})
}

func (s *server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	raw = bytes.TrimSpace(raw)
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(raw)
	if !hmac.Equal([]byte(r.Header.Get("X-Signature")), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad signature"})
		return
	}
	var body struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	// Latency staggered per method so the four checkout matrix combinations
	// don't all land on the report within the same instant: card clears fast,
	// crypto waits on confirmations, bank transfers are slowest.
	switch body.Method {
	case "card":
		latency(250, 500)
		writeJSON(w, http.StatusOK, map[string]string{"status": "paid"})
	case "paypal":
		latency(500, 850)
		writeJSON(w, http.StatusOK, map[string]string{"status": "paid"})
	case "crypto":
		latency(900, 1400)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending_confirmation"})
	default:
		latency(1300, 1900)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending_confirmation"})
	}
}

func (s *server) handleShipment(w http.ResponseWriter, r *http.Request) {
	latency(400, 1300)
	id := r.PathValue("id")
	s.mu.Lock()
	s.shipmentAt[id]++
	polls := s.shipmentAt[id]
	s.mu.Unlock()
	status := "packing"
	if polls >= 3 {
		status = "delivered"
	}
	eta := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	writeJSON(w, http.StatusOK, map[string]string{"status": status, "eta": eta})
}

func (s *server) handleReserveStock(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	var body struct {
		SKU string `json:"sku"`
		Qty int    `json:"qty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	s.mu.Lock()
	stock, ok := s.inventory[body.SKU]
	if !ok {
		stock = 100
	}
	stock -= body.Qty
	s.inventory[body.SKU] = stock
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"reserved": true, "remaining": stock})
}

func (s *server) handleGetStock(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	sku := r.PathValue("sku")
	s.mu.Lock()
	stock, ok := s.inventory[sku]
	s.mu.Unlock()
	if !ok {
		stock = 100
	}
	writeJSON(w, http.StatusOK, map[string]any{"sku": sku, "stock": stock})
}

func (s *server) handleSendNotification(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	id := newID("notif")
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "queued"})
}

func (s *server) handleNotificationStatus(w http.ResponseWriter, r *http.Request) {
	latency(300, 1000)
	id := r.PathValue("id")
	s.mu.Lock()
	s.notifAt[id]++
	polls := s.notifAt[id]
	s.mu.Unlock()
	status := "queued"
	if polls >= 2 {
		status = "delivered"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	latency(200, 650)
	q := r.URL.Query().Get("q")
	if page := r.URL.Query().Get("page"); page != "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"results": []string{q + "_page_" + page},
			"next":    page == "1",
		})
		return
	}
	s.mu.Lock()
	s.searchAt[q]++
	polls := s.searchAt[q]
	s.mu.Unlock()
	if polls < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"indexed": false, "results": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"indexed": true, "results": []string{q + "_result_1", q + "_result_2"}})
}

func (s *server) handleAdminWipe(w http.ResponseWriter, r *http.Request) {
	latency(150, 450)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "insufficient_scope"})
}

func (s *server) handleCreateReport(w http.ResponseWriter, r *http.Request) {
	latency(200, 550)
	id := newID("rpt")
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "processing"})
}

func (s *server) handleExportReport(w http.ResponseWriter, r *http.Request) {
	latency(400, 1100)
	id := r.PathValue("id")
	s.mu.Lock()
	s.reportAt[id]++
	polls := s.reportAt[id]
	s.mu.Unlock()
	if polls < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "processing"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "complete", "records": 128})
}

func main() {
	s := newServer()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /session/refresh", s.requireAuth(s.handleRefresh))
	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("POST /users", s.requireAuth(s.handleCreateUser))
	mux.HandleFunc("GET /users/{id}", s.requireAuth(s.handleGetUser))
	mux.HandleFunc("PATCH /users/{id}", s.requireAuth(s.handleUpdateUser))
	mux.HandleFunc("PATCH /users/{id}/password", s.requireAuth(s.handleChangePassword))
	mux.HandleFunc("POST /users/{id}/wishlist", s.requireAuth(s.handleWishlist))
	mux.HandleFunc("POST /users/{id}/avatar", s.requireAuth(s.handleAvatar))

	mux.HandleFunc("GET /products", s.requireAuth(s.handleProducts))
	mux.HandleFunc("GET /catalog/featured", s.requireAuth(s.handleFeatured))

	mux.HandleFunc("POST /orders", s.requireAuth(s.handleCreateOrder))
	mux.HandleFunc("POST /orders/{id}/checkout", s.requireAuth(s.handleCheckout))
	mux.HandleFunc("GET /orders/{id}/shipment", s.requireAuth(s.handleShipment))

	mux.HandleFunc("GET /inventory/{sku}", s.requireAuth(s.handleGetStock))
	mux.HandleFunc("POST /inventory/reserve", s.requireAuth(s.handleReserveStock))

	mux.HandleFunc("POST /notifications", s.requireAuth(s.handleSendNotification))
	mux.HandleFunc("GET /notifications/{id}", s.requireAuth(s.handleNotificationStatus))

	mux.HandleFunc("GET /search", s.requireAuth(s.handleSearch))

	mux.HandleFunc("POST /admin/wipe", s.requireAuth(s.handleAdminWipe))

	mux.HandleFunc("POST /reports", s.requireAuth(s.handleCreateReport))
	mux.HandleFunc("GET /reports/{id}/export", s.requireAuth(s.handleExportReport))

	addr := "localhost:8085"
	log.Printf("sinq demo server listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
