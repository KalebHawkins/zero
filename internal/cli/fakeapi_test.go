package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/KalebHawkins/zero/spec"
)

// fakeAPI implements the endpoints of the platform's SPEC section 4 that the
// command uses. It serves one exercise and one user, and it approves a login
// code by itself after pendingPolls polls.
type fakeAPI struct {
	mu sync.Mutex

	prefix   string // path prefix of the site root, for example "/zero-next"
	siteRoot string // set once the server listens

	exercise spec.Exercise
	files    []spec.File
	readme   string
	user     spec.User
	token    string
	next     *spec.Next // what comes after the exercise

	pendingPolls int  // 202 answers before the code is approved
	expired      bool // the code is expired: the token poll answers 410
	revoked      bool // POST /api/cli/logout ended the token; a new login issues it again
	logoutStatus int  // when not 0, POST /api/cli/logout answers this error status

	// What the command sent.
	devices    []spec.DeviceRequest
	polls      int
	started    []string
	runs       []spec.Report
	submits    []spec.SubmitRequest
	doctors    []spec.Doctor
	logouts    []string // the Authorization header of every POST /api/cli/logout
	passed     bool
	userAgents map[string]bool

	// More exercises and project stages, beside the main exercise above.
	entries    map[string]*fakeEntry
	projects   map[string]string // project id -> title
	passedIDs  map[string]bool   // entries that were submitted
	references []string          // ids requested with ?reference=1
}

// fakeEntry is one more exercise, or a stage of a project.
type fakeEntry struct {
	exercise spec.Exercise
	files    []spec.File
	readme   string
	solution []spec.File // solution/, laid out like starter/

	project string // empty for a plain exercise
	stage   int
	uses    []spec.Use
}

// newFakeAPI loads the exercise from a folder laid out like the platform's
// content: exercise.json and starter/. A file named go.mod.txt is served as
// go.mod; a real go.mod cannot live in testdata that is embedded.
func newFakeAPI(content fs.FS, prefix string) (*fakeAPI, error) {
	f := &fakeAPI{
		prefix:     prefix,
		user:       spec.User{ID: "github:123", Login: "kryo", Name: "Kaleb", Avatar: "https://example.org/a.png"},
		token:      "fake-token-1",
		next:       &spec.Next{Kind: "exercise", ID: "variables", Title: "Variables"},
		userAgents: map[string]bool{},
		entries:    map[string]*fakeEntry{},
		projects:   map[string]string{},
		passedIDs:  map[string]bool{},
	}
	e, err := loadEntry(content)
	if err != nil {
		return nil, err
	}
	f.exercise, f.files, f.readme = e.exercise, e.files, e.readme
	return f, nil
}

// addContent loads every folder of content that holds an exercise.json as
// one more exercise or stage, and project.json as the project's title.
func (f *fakeAPI) addContent(content fs.FS) error {
	if raw, err := fs.ReadFile(content, "project.json"); err == nil {
		var p struct{ ID, Title string }
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		f.projects[p.ID] = p.Title
	}
	dirs, err := fs.ReadDir(content, ".")
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		sub, err := fs.Sub(content, d.Name())
		if err != nil {
			return err
		}
		e, err := loadEntry(sub)
		if err != nil {
			return fmt.Errorf("%s: %w", d.Name(), err)
		}
		f.entries[e.exercise.ID] = e
	}
	return nil
}

// loadEntry reads exercise.json, starter/ and solution/ of one exercise.
func loadEntry(content fs.FS) (*fakeEntry, error) {
	e := &fakeEntry{}
	raw, err := fs.ReadFile(content, "exercise.json")
	if err != nil {
		return nil, err
	}
	// exercise.json has more fields than the API sends. Decoding into
	// spec.Exercise keeps exactly the fields of the SPEC.
	if err := json.Unmarshal(raw, &e.exercise); err != nil {
		return nil, err
	}
	var stage struct {
		Project string     `json:"project"`
		Stage   int        `json:"stage"`
		Uses    []spec.Use `json:"uses"`
	}
	if err := json.Unmarshal(raw, &stage); err != nil {
		return nil, err
	}
	e.project, e.stage, e.uses = stage.Project, stage.Stage, stage.Uses
	edit := map[string]bool{}
	for _, p := range e.exercise.Edit {
		edit[p] = true
	}
	if e.files, err = readTree(content, "starter", edit); err != nil {
		return nil, err
	}
	if e.solution, err = readTree(content, "solution", edit); err != nil {
		return nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\n%s\n\n", e.exercise.Title, strings.Repeat("=", len(e.exercise.Title)), e.exercise.Lead)
	for _, t := range e.exercise.Tasks {
		fmt.Fprintf(&b, "Task %d: %s\n  %s\n  Checked by %s\n\n", t.N, t.Title, t.Text, strings.Join(t.Tests, ", "))
	}
	b.WriteString("Commands\n  zero test     run the tests\n  zero run      see it\n  zero hint     show a hint\n  zero submit   finish, when every test passes\n")
	e.readme = b.String()
	return e, nil
}

// readTree reads the files under dir. A missing dir is an empty list.
func readTree(content fs.FS, dir string, edit map[string]bool) ([]spec.File, error) {
	var files []spec.File
	if _, err := fs.Stat(content, dir); err != nil {
		return nil, nil
	}
	err := fs.WalkDir(content, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(content, p)
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(p, dir+"/")
		if path.Base(name) == "go.mod.txt" {
			name = strings.TrimSuffix(name, ".txt")
		}
		mode := spec.ModeGiven
		if edit[name] {
			mode = spec.ModeEdit
		}
		files = append(files, spec.File{Path: name, Content: string(b), Mode: mode})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", f.config)
	mux.HandleFunc("GET /api/me", f.bearer(f.me))
	mux.HandleFunc("POST /api/cli/device", f.device)
	mux.HandleFunc("POST /api/cli/token", f.cliToken)
	mux.HandleFunc("POST /api/cli/logout", f.cliLogout)
	mux.HandleFunc("POST /api/doctor", f.bearer(f.doctor))
	mux.HandleFunc("GET /api/exercises/{id}", f.bearer(f.getExercise))
	mux.HandleFunc("POST /api/exercises/{id}/runs", f.bearer(f.postRun))
	mux.HandleFunc("POST /api/exercises/{id}/submit", f.bearer(f.submit))
	mux.HandleFunc("GET /api/next", f.bearer(f.getNext))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sendError(w, http.StatusNotFound, "not_found", "Nothing is at "+r.URL.Path+".")
	})
	counted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.userAgents[r.Header.Get("User-Agent")] = true
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	})
	if f.prefix == "" {
		return counted
	}
	outer := http.NewServeMux()
	outer.Handle(f.prefix+"/", http.StripPrefix(f.prefix, counted))
	return outer
}

func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func sendError(w http.ResponseWriter, status int, code, message string) {
	send(w, status, spec.Error{Code: code, Message: message})
}

// receive decodes a JSON body, or answers 400.
func receive(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		sendError(w, http.StatusBadRequest, "bad_request", "The body is not the expected JSON: "+err.Error())
		return false
	}
	return true
}

func (f *fakeAPI) bearer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		revoked := f.revoked
		f.mu.Unlock()
		if revoked || r.Header.Get("Authorization") != "Bearer "+f.token {
			sendError(w, http.StatusUnauthorized, "unauthorized", "This sign-in is not valid")
			return
		}
		next(w, r)
	}
}

func (f *fakeAPI) config(w http.ResponseWriter, r *http.Request) {
	send(w, http.StatusOK, spec.ServerConfig{DevLogin: true, GitHub: false})
}

func (f *fakeAPI) me(w http.ResponseWriter, r *http.Request) {
	send(w, http.StatusOK, spec.MeResponse{User: f.user})
}

func (f *fakeAPI) device(w http.ResponseWriter, r *http.Request) {
	var in spec.DeviceRequest
	if !receive(w, r, &in) {
		return
	}
	f.mu.Lock()
	f.devices = append(f.devices, in)
	f.polls = 0
	root := f.siteRoot
	f.mu.Unlock()
	send(w, http.StatusOK, spec.DeviceResponse{
		DeviceCode:              "device-code-1",
		UserCode:                "ABCD-EFGH",
		VerificationURI:         root + "/cli/",
		VerificationURIComplete: root + "/cli/?code=ABCD-EFGH",
		Interval:                2,
		ExpiresIn:               600,
	})
}

func (f *fakeAPI) cliToken(w http.ResponseWriter, r *http.Request) {
	var in spec.TokenRequest
	if !receive(w, r, &in) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	switch {
	case in.DeviceCode != "device-code-1":
		sendError(w, http.StatusNotFound, "not_found", "There is no such code.")
	case f.expired:
		sendError(w, http.StatusGone, "expired", "The code expired.")
	case f.polls <= f.pendingPolls:
		send(w, http.StatusAccepted, map[string]string{"status": "pending"})
	default:
		f.revoked = false
		send(w, http.StatusOK, spec.TokenResponse{Token: f.token, User: f.user})
	}
}

// cliLogout ends the bearer token and answers 204.
func (f *fakeAPI) cliLogout(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	auth := r.Header.Get("Authorization")
	f.logouts = append(f.logouts, auth)
	switch {
	case f.logoutStatus != 0:
		sendError(w, f.logoutStatus, "broken", "The site cannot end tokens right now.")
	case f.revoked || auth != "Bearer "+f.token:
		sendError(w, http.StatusUnauthorized, "unauthorized", "This sign-in is not valid")
	default:
		f.revoked = true
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeAPI) doctor(w http.ResponseWriter, r *http.Request) {
	var in spec.Doctor
	if !receive(w, r, &in) {
		return
	}
	f.mu.Lock()
	f.doctors = append(f.doctors, in)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeAPI) known(w http.ResponseWriter, r *http.Request) bool {
	if id := r.PathValue("id"); id != f.exercise.ID && f.entries[id] == nil {
		sendError(w, http.StatusNotFound, "not_found", fmt.Sprintf("There is no exercise with the id %q.", id))
		return false
	}
	return true
}

func (f *fakeAPI) getExercise(w http.ResponseWriter, r *http.Request) {
	if !f.known(w, r) {
		return
	}
	id := r.PathValue("id")
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.entries[id]
	if e == nil {
		f.started = append(f.started, f.exercise.ID)
		send(w, http.StatusOK, spec.ExerciseResponse{Exercise: f.exercise, Files: f.files, Readme: f.readme})
		return
	}
	resp := spec.ExerciseResponse{Exercise: e.exercise, Files: e.files, Readme: e.readme}
	if e.project != "" {
		title := f.projects[e.project]
		prev := fmt.Sprintf("%s-%d", e.project, e.stage-1)
		if e.stage > 1 && !f.passedIDs[prev] {
			sendError(w, http.StatusConflict, "stage_locked",
				fmt.Sprintf("Stage %d of %s opens when stage %d is passed. Finish it first: zero start %s", e.stage, title, e.stage-1, prev))
			return
		}
		p := &spec.Project{ID: e.project, Title: title, Stage: e.stage, EditAll: []string{}}
		seen := map[string]bool{}
		for _, other := range f.entries {
			if other.project == e.project {
				p.Stages = max(p.Stages, other.stage)
			}
		}
		for n := 1; n <= e.stage; n++ {
			for _, path := range f.entries[fmt.Sprintf("%s-%d", e.project, n)].exercise.Edit {
				if !seen[path] {
					seen[path] = true
					p.EditAll = append(p.EditAll, path)
				}
			}
		}
		resp.Project, resp.Uses = p, e.uses
		if r.URL.Query().Get("reference") == "1" {
			f.references = append(f.references, id)
			if e.stage > 1 {
				resp.Reference = f.entries[prev].solution
			}
		}
	}
	f.started = append(f.started, id)
	send(w, http.StatusOK, resp)
}

func (f *fakeAPI) postRun(w http.ResponseWriter, r *http.Request) {
	if !f.known(w, r) {
		return
	}
	var in spec.Report
	if !receive(w, r, &in) {
		return
	}
	f.mu.Lock()
	f.runs = append(f.runs, in)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeAPI) submit(w http.ResponseWriter, r *http.Request) {
	if !f.known(w, r) {
		return
	}
	var in spec.SubmitRequest
	if !receive(w, r, &in) {
		return
	}
	if !in.Report.OK {
		sendError(w, http.StatusUnprocessableEntity, "not_passing", "The tests in this report do not pass.")
		return
	}
	f.mu.Lock()
	f.submits = append(f.submits, in)
	if id := r.PathValue("id"); f.entries[id] != nil {
		f.passedIDs[id] = true
	} else {
		f.passed = true
	}
	next := f.next
	f.mu.Unlock()
	send(w, http.StatusOK, spec.SubmitResponse{Passed: true, Next: next})
}

func (f *fakeAPI) getNext(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.passed {
		send(w, http.StatusOK, spec.NextResponse{Next: &spec.Next{Kind: "exercise", ID: f.exercise.ID, Title: f.exercise.Title}})
		return
	}
	send(w, http.StatusOK, spec.NextResponse{Next: f.next})
}
