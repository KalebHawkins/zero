// Package spec holds the JSON types shared by the zero command and the
// Zero Series platform. The shapes are fixed by the platform's docs/SPEC.md.
// The package has no dependencies, so the API and the site can import it.
package spec

// Status values of a test and of a task in a Report.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// Mode values of a File.
const (
	ModeEdit  = "edit"
	ModeGiven = "given"
)

// CheckerGoTest is the checker that runs `go test -json ./...`.
const CheckerGoTest = "go-test"

// Messages the command writes into a Report for tests that produced no result.
const (
	MessageDidNotRun     = "this test did not run"
	MessageDidNotCompile = "the code did not compile"
)

// Exercise is an exercise as the API sends it.
type Exercise struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Lead    string   `json:"lead"`
	Minutes int      `json:"minutes"`
	Checker string   `json:"checker"`
	Edit    []string `json:"edit"`
	Run     []string `json:"run"`
	Tasks   []Task   `json:"tasks"`
	Hints   []Hint   `json:"hints"`
}

// Task is one step of an exercise. It passes when all of its tests pass.
type Task struct {
	N     int      `json:"n"`
	Title string   `json:"title"`
	Text  string   `json:"text"`
	Tests []string `json:"tests"`
}

// Hint is one hint in plain text.
type Hint struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// File is one file of an exercise. Mode is "edit" or "given".
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    string `json:"mode,omitempty"`
}

// Next names what a learner does next. A nil *Next means the path has
// nothing more yet.
type Next struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Report is the result of one test run. Regressions is filled only in a
// project: it lists the failing tests that no task of the current stage
// names, so an earlier stage broke. OK is false when it is not empty.
type Report struct {
	Exercise    string       `json:"exercise"`
	Checker     string       `json:"checker"`
	OK          bool         `json:"ok"`
	At          string       `json:"at"` // RFC 3339, UTC
	DurationMS  int64        `json:"duration_ms"`
	CLI         string       `json:"cli"`
	BuildError  string       `json:"build_error"`
	Tasks       []TaskResult `json:"tasks"`
	Regressions []TestResult `json:"regressions,omitempty"`
}

// TaskResult is the result of one task in a Report.
type TaskResult struct {
	N      int          `json:"n"`
	Title  string       `json:"title"`
	Status string       `json:"status"`
	Tests  []TestResult `json:"tests"`
}

// TestResult is the result of one test in a Report: a test of a task, or a
// regression.
type TestResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Doctor is the result of `zero doctor`.
type Doctor struct {
	OK     bool    `json:"ok"`
	At     string  `json:"at"` // RFC 3339, UTC
	CLI    string  `json:"cli"`
	Checks []Check `json:"checks"`
}

// Check is one line of a Doctor. Fix is one sentence that says how to make
// the check pass; it is empty when OK is true. An optional check that is not
// OK is a note: it does not make the Doctor fail.
type Check struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix"`
	Optional bool   `json:"optional,omitempty"`
}

// User is a signed-in person.
type User struct {
	ID     string `json:"id"`
	Login  string `json:"login"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// Error is the body of every 4xx and 5xx answer.
type Error struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

// ServerConfig is the answer of GET /api/config.
type ServerConfig struct {
	DevLogin bool `json:"dev_login"`
	GitHub   bool `json:"github"`
}

// MeResponse is the answer of GET /api/me.
type MeResponse struct {
	User User `json:"user"`
}

// DeviceRequest is the body of POST /api/cli/device.
type DeviceRequest struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Version  string `json:"version"`
}

// DeviceResponse is the answer of POST /api/cli/device.
type DeviceResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`   // seconds between polls
	ExpiresIn               int    `json:"expires_in"` // seconds until the code expires
}

// TokenRequest is the body of POST /api/cli/token.
type TokenRequest struct {
	DeviceCode string `json:"device_code"`
}

// TokenResponse is the 200 answer of POST /api/cli/token.
type TokenResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// ExerciseResponse is the answer of GET /api/exercises/{id}. Project is set
// only for a stage of a project. Reference is sent only with ?reference=1:
// the whole project as it stands after the previous stage.
type ExerciseResponse struct {
	Exercise  Exercise `json:"exercise"`
	Files     []File   `json:"files"`
	Readme    string   `json:"readme"`
	Project   *Project `json:"project,omitempty"`
	Uses      []Use    `json:"uses,omitempty"`
	Reference []File   `json:"reference,omitempty"`
}

// Project says where a stage stands in its project. All stages of a project
// share the folder <workspace>/<ID>/. EditAll is every edit file of stages 1
// to Stage; these are the files `zero submit` uploads.
type Project struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Stage   int      `json:"stage"`
	Stages  int      `json:"stages"`
	EditAll []string `json:"edit_all"`
}

// Use names a finished exercise whose files a stage may copy into the
// project with `zero use`.
type Use struct {
	Exercise string `json:"exercise"`
	Copy     []Copy `json:"copy"`
}

// Copy is one file of a Use. From is a path in the exercise folder; To is a
// path in the project folder.
type Copy struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// SubmitRequest is the body of POST /api/exercises/{id}/submit.
type SubmitRequest struct {
	Report Report `json:"report"`
	Files  []File `json:"files"`
}

// SubmitResponse is the 200 answer of POST /api/exercises/{id}/submit.
type SubmitResponse struct {
	Passed bool  `json:"passed"`
	Next   *Next `json:"next"`
}

// NextResponse is the answer of GET /api/next.
type NextResponse struct {
	Next *Next `json:"next"`
}

// SolutionResponse is the answer of GET /api/exercises/{id}/solution.
type SolutionResponse struct {
	Files     []File `json:"files"`
	AfterHTML string `json:"after_html"`
}

// State is the answer of GET /api/state.
type State struct {
	User      User                     `json:"user"`
	Path      string                   `json:"path"`
	Setup     Setup                    `json:"setup"`
	Exercises map[string]ExerciseState `json:"exercises"`
}

// Setup is the setup part of a State.
type Setup struct {
	GoConfirmed bool    `json:"go_confirmed"`
	CLILinked   bool    `json:"cli_linked"`
	Doctor      *Doctor `json:"doctor"`
}

// ExerciseState is one learner's progress on one exercise. Status is
// "started" or "passed".
type ExerciseState struct {
	Status    string  `json:"status"`
	StartedAt string  `json:"started_at"`
	PassedAt  string  `json:"passed_at"`
	LastRun   *Report `json:"last_run"`
}

// Saved is the content of .zero/exercise.json in an exercise folder: the
// exercise, and the path and mode of every file `zero start` wrote. In a
// project folder it describes the current stage, and Project and Uses hold
// what the API sent for that stage.
type Saved struct {
	Exercise Exercise  `json:"exercise"`
	Files    []FileRef `json:"files"`
	Project  *Project  `json:"project,omitempty"`
	Uses     []Use     `json:"uses,omitempty"`
}

// FileRef is a File without its content.
type FileRef struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}
