package download

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JeremiahM37/librarr/internal/config"
)

type mockDeluge struct {
	labels        []string
	labelEnabled  bool
	lastMethod    string
	lastParams    []interface{}
	lastLabelHash string
	lastLabel     string
	removedHash   string
	removedData   bool
}

func (m *mockDeluge) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req delugeRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.lastMethod = req.Method
		m.lastParams = req.Params

		write := func(result interface{}, rpcErr interface{}) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": req.ID, "result": result, "error": rpcErr,
			})
		}

		switch req.Method {
		case "auth.login":
			http.SetCookie(w, &http.Cookie{Name: "_session_id", Value: "test-session", Path: "/"})
			write(true, nil)
		case "label.get_labels":
			if !m.labelEnabled {
				write(nil, map[string]interface{}{"code": 1, "message": "Unknown method: label.get_labels"})
				return
			}
			write(m.labels, nil)
		case "label.add":
			label, _ := req.Params[0].(string)
			m.labels = append(m.labels, label)
			write(nil, nil)
		case "label.set_torrent":
			m.lastLabelHash, _ = req.Params[0].(string)
			m.lastLabel, _ = req.Params[1].(string)
			write(nil, nil)
		case "core.add_torrent_magnet", "core.add_torrent_url":
			write("h1", nil)
		case "core.get_torrents_status":
			write(map[string]interface{}{
				"h1": map[string]interface{}{
					"name": "Book One", "state": "Seeding", "progress": 100.0,
					"total_size": 1000, "download_payload_rate": 0,
					"save_path": "/downloads", "label": "books",
				},
				"h2": map[string]interface{}{
					"name": "Book Two", "state": "Downloading", "progress": 25.0,
					"total_size": 2000, "download_payload_rate": 250,
					"save_path": "/downloads", "label": "books",
				},
			}, nil)
		case "core.get_torrent_status":
			write(map[string]interface{}{
				"files": []map[string]interface{}{
					{"path": "Book/chapter1.epub", "size": 100},
					{"path": "Book/chapter2.epub", "size": 200},
				},
			}, nil)
		case "core.remove_torrent":
			m.removedHash, _ = req.Params[0].(string)
			m.removedData, _ = req.Params[1].(bool)
			write(true, nil)
		default:
			write(nil, map[string]interface{}{"code": 1, "message": "unknown method"})
		}
	}
}

func newTestDeluge(url string) *DelugeClient {
	return NewDelugeClient(&config.Config{
		DelugeURL:      url,
		DelugePassword: "deluge",
		QBSavePath:     "/downloads",
		QBCategory:     "books",
	})
}

func TestDeluge_DiagnoseAndLabelPlugin(t *testing.T) {
	mock := &mockDeluge{labelEnabled: true, labels: []string{"books"}}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	d := newTestDeluge(srv.URL)
	res := d.Diagnose()
	if res["success"] != true {
		t.Fatalf("Diagnose expected success, got %#v", res)
	}
	if d.cookie == "" {
		t.Error("expected Deluge session cookie to be stored")
	}
}

func TestDeluge_DiagnoseRequiresLabelPlugin(t *testing.T) {
	mock := &mockDeluge{labelEnabled: false}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	res := newTestDeluge(srv.URL).Diagnose()
	if res["success"] != false {
		t.Fatalf("expected failure without Label plugin, got %#v", res)
	}
}

func TestDeluge_DiagnoseNotConfigured(t *testing.T) {
	res := NewDelugeClient(&config.Config{}).Diagnose()
	if res["success"] != false {
		t.Fatalf("expected failure when not configured, got %#v", res)
	}
}

func TestDeluge_AddTorrentSetsPathAndLabel(t *testing.T) {
	mock := &mockDeluge{labelEnabled: true, labels: []string{"books"}}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	d := newTestDeluge(srv.URL)
	if err := d.AddTorrent("magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "Some Book", "/incoming", "audiobooks", ""); err != nil {
		t.Fatalf("AddTorrent error: %v", err)
	}
	if mock.lastLabelHash != "h1" || mock.lastLabel != "audiobooks" {
		t.Fatalf("label assignment = %q/%q, want h1/audiobooks", mock.lastLabelHash, mock.lastLabel)
	}
	found := false
	for _, l := range mock.labels {
		if l == "audiobooks" {
			found = true
		}
	}
	if !found {
		t.Error("expected missing audiobook label to be created")
	}
}

func TestDeluge_AddTorrentFallsBackToDefaults(t *testing.T) {
	mock := &mockDeluge{labelEnabled: true, labels: []string{"books"}}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	d := newTestDeluge(srv.URL)
	if err := d.AddTorrent("https://example.invalid/book.torrent", "Book", "", "", ""); err != nil {
		t.Fatalf("AddTorrent error: %v", err)
	}
	if mock.lastLabel != "books" {
		t.Errorf("default label = %q, want books", mock.lastLabel)
	}
}

func TestDeluge_GetTorrentsMapsStateAndProgress(t *testing.T) {
	mock := &mockDeluge{labelEnabled: true, labels: []string{"books"}}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	got, err := newTestDeluge(srv.URL).GetTorrents("books")
	if err != nil {
		t.Fatalf("GetTorrents error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 torrents, got %d: %#v", len(got), got)
	}
	byHash := map[string]TorrentInfo{}
	for _, ti := range got {
		byHash[ti.Hash] = ti
	}
	if MapTorrentStatus(byHash["h1"].State) != "completed" {
		t.Errorf("seeding state = %q -> %q", byHash["h1"].State, MapTorrentStatus(byHash["h1"].State))
	}
	if MapTorrentStatus(byHash["h2"].State) != "downloading" {
		t.Errorf("download state = %q -> %q", byHash["h2"].State, MapTorrentStatus(byHash["h2"].State))
	}
	if byHash["h2"].Progress != 0.25 {
		t.Errorf("Progress = %v, want 0.25", byHash["h2"].Progress)
	}
	if byHash["h1"].ContentPath != "/downloads/Book One" {
		t.Errorf("ContentPath = %q", byHash["h1"].ContentPath)
	}
}

func TestDeluge_GetTorrentFiles(t *testing.T) {
	mock := &mockDeluge{labelEnabled: true}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	files, err := newTestDeluge(srv.URL).GetTorrentFiles("h1")
	if err != nil {
		t.Fatalf("GetTorrentFiles error: %v", err)
	}
	if len(files) != 2 || files[0].Name != "Book/chapter1.epub" {
		t.Fatalf("unexpected files: %#v", files)
	}
}

func TestDeluge_DeleteTorrent(t *testing.T) {
	mock := &mockDeluge{labelEnabled: true}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	if err := newTestDeluge(srv.URL).DeleteTorrent("h1", true); err != nil {
		t.Fatalf("DeleteTorrent error: %v", err)
	}
	if mock.removedHash != "h1" || !mock.removedData {
		t.Errorf("remove args = %q/%v", mock.removedHash, mock.removedData)
	}
}

func TestMapDelugeState(t *testing.T) {
	cases := []struct {
		state    string
		progress float64
		want     string
	}{
		{"Downloading", 0.3, "downloading"},
		{"Seeding", 1, "uploading"},
		{"Paused", 0.3, "pausedDL"},
		{"Paused", 1, "pausedUP"},
		{"Queued", 0.3, "queuedDL"},
		{"Queued", 1, "queuedUP"},
		{"Checking", 0.5, "checkingDL"},
		{"Checking", 1, "checkingUP"},
		{"Error", 0.2, "error"},
		{"Moving", 1, "downloading"},
	}
	for _, c := range cases {
		if got := mapDelugeState(c.state, c.progress); got != c.want {
			t.Errorf("mapDelugeState(%q,%v) = %q, want %q", c.state, c.progress, got, c.want)
		}
	}
}

func TestSelectTorrentClientDeluge(t *testing.T) {
	cfg := &config.Config{DelugeURL: "http://deluge", TorrentClient: "deluge"}
	qb := NewQBittorrentClient(cfg)
	tr := NewTransmissionClient(cfg)
	de := NewDelugeClient(cfg)
	got := SelectTorrentClient(cfg, qb, tr, de)
	if got == nil || got.Name() != "deluge" {
		t.Fatalf("selected client = %#v, want deluge", got)
	}
}
