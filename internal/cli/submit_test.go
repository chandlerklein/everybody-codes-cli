package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chandlerklein/everybody-codes-cli/internal/everybodycodes"
)

type submissionRequest struct {
	calls  int
	url    string
	answer string
	inputs *testAPI
}

func setupSubmission(t *testing.T, status int, body string) *submissionRequest {
	t.Helper()
	api := setup(t)
	downloadTransport := apiClient.Transport
	request := &submissionRequest{inputs: api}
	apiClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			if request.calls != 1 {
				t.Error("download started before submission completed")
			}
			return downloadTransport.RoundTrip(r)
		}
		request.calls++
		request.url = r.URL.String()
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected content negotiation headers: %v", r.Header)
		}
		cookie, err := r.Cookie("everybody-codes")
		if err != nil || cookie.Value != testSession {
			t.Error("submission did not include the expected session cookie")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			t.Error("submission has no bounded timeout")
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("invalid JSON payload: %v", err)
		}
		if len(payload) != 1 {
			t.Errorf("payload has %d fields, want only answer", len(payload))
		}
		if err := json.Unmarshal(payload["answer"], &request.answer); err != nil {
			t.Errorf("answer is not a JSON string: %v", err)
		}
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	return request
}

func executeSubmit(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	return execute(ctx, append([]string{"submit"}, args...), input, output, "dev", everybodycodes.NewClient(apiClient))
}

func TestSubmitArgumentsAndInput(t *testing.T) {
	for _, tt := range []struct {
		name   string
		args   []string
		input  string
		year   string
		quest  int
		part   int
		answer string
	}{
		{"short flags", []string{"-y", "2024", "-q", "1", "-p", "1", "1309"}, "", "2024", 1, 1, "1309"},
		{"long flags", []string{"--year=2024", "--quest=08", "--part=3", "001309"}, "", "2024", 8, 3, "001309"},
		{"current year", []string{"-q", "1", "-p", "1", "1309"}, "", fmt.Sprint(time.Now().Year()), 1, 1, "1309"},
		{"current year long flags", []string{"--quest", "1", "--part", "1", "1309"}, "", fmt.Sprint(time.Now().Year()), 1, 1, "1309"},
		{"year identifier", []string{"-y", "story-1", "-q", "1", "-p", "1", "1309"}, "", "story-1", 1, 1, "1309"},
		{"positive part beyond three", []string{"-y2024", "-q00010", "-p8", "1309"}, "", "2024", 10, 8, "1309"},
		{"text and escaping", []string{"-y2024", "-q1", "-p1", " café ☃ 🐈 e\u0301 \"quoted\"\\\t\x00\n"}, "", "2024", 1, 1, " café ☃ 🐈 e\u0301 \"quoted\"\\\t\x00\n"},
		{"answer before flags", []string{"1309", "-y2024", "-q1", "-p1"}, "", "2024", 1, 1, "1309"},
		{"negative answer", []string{"-y2024", "-q1", "-p1", "--", "-42"}, "", "2024", 1, 1, "-42"},
		{"stdin LF", []string{"-y2024", "-q1", "-p1"}, "1309\n", "2024", 1, 1, "1309"},
		{"stdin CRLF", []string{"-y2024", "-q1", "-p1"}, "1309\r\n", "2024", 1, 1, "1309"},
		{"stdin without newline", []string{"-y2024", "-q1", "-p1"}, "résultat 🐈", "2024", 1, 1, "résultat 🐈"},
		{"stdin preserves whitespace", []string{"-y2024", "-q1", "-p1"}, " \tα\nβ \n\n", "2024", 1, 1, " \tα\nβ \n"},
		{"argument takes precedence", []string{"-y2024", "-q1", "-p1", "1309"}, "\xffinvalid stdin", "2024", 1, 1, "1309"},
		{"long stdin", []string{"-y2024", "-q1", "-p1"}, strings.Repeat("é", 40000) + "\n", "2024", 1, 1, strings.Repeat("é", 40000)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := setupSubmission(t, http.StatusOK, `{"correct":false}`)
			var output bytes.Buffer
			if err := executeSubmit(t.Context(), tt.args, strings.NewReader(tt.input), &output); err != nil {
				t.Fatal(err)
			}
			wantURL := fmt.Sprintf("%s/event/%s/quest/%d/part/%d/answer", apiBaseURL, tt.year, tt.quest, tt.part)
			if request.calls != 1 || request.url != wantURL || request.answer != tt.answer {
				t.Fatalf("calls = %d, URL = %s, answer matches = %t", request.calls, request.url, request.answer == tt.answer)
			}
			wantOutput := fmt.Sprintf("Year %s, quest %d, part %d\nHTTP 200 OK\n{\"correct\":false}\n", tt.year, tt.quest, tt.part)
			if got, want := output.String(), wantOutput; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
			entries, err := os.ReadDir(".")
			if err != nil || len(entries) != 0 {
				t.Fatalf("submit created files: entries = %v, error = %v", entries, err)
			}
		})
	}
}

func TestSubmitRejectsInvalidArgumentsBeforeRequests(t *testing.T) {
	for _, tt := range []struct {
		args  []string
		input string
		want  string
	}{
		{nil, "1309", "--quest (-q) and --part (-p) required"},
		{[]string{"-q1", "1309"}, "", "--part (-p) required"},
		{[]string{"-p1", "1309"}, "", "--quest (-q) required"},
		{[]string{"-q1", "-p1", "1309", "extra"}, "", "at most 1 arg(s)"},
		{[]string{"-q1", "-p1", "--year="}, "1309", "year must be"},
		{[]string{"-q1", "-p1", "--year=../2024"}, "1309", "year must be"},
		{[]string{"-q1", "-p1", "--year=2024?query"}, "1309", "year must be"},
		{[]string{"-q1", "-p1"}, "", "answer must not be empty"},
		{[]string{"-q1", "-p1"}, " \t\r\n", "answer must not be empty"},
		{[]string{"-q1", "-p1", ""}, "1309", "answer must not be empty"},
		{[]string{"-q1", "-p1", " \n"}, "", "answer must not be empty"},
		{[]string{"-q1", "-p1", "\xff"}, "", "answer must be valid UTF-8"},
		{[]string{"-q1", "-p1"}, "\xc3\n", "answer must be valid UTF-8"},
		{[]string{"-q1", "-p1", "--dir=elsewhere", "1309"}, "", "unknown flag"},
		{[]string{"-q1", "-p1", "--force", "1309"}, "", "unknown flag"},
		{[]string{"-q1", "-p"}, "1309", "flag needs an argument"},
	} {
		t.Run(fmt.Sprint(tt.args, tt.input), func(t *testing.T) {
			request := setupSubmission(t, http.StatusOK, "")
			var output bytes.Buffer
			err := executeSubmit(t.Context(), tt.args, strings.NewReader(tt.input), &output)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if request.calls != 0 || output.Len() != 0 {
				t.Fatalf("invalid input caused %d requests or output %q", request.calls, output.String())
			}
		})
	}
	for _, flag := range []string{"quest", "part"} {
		invalid := []string{"", "0", "-1", "+1", "1.5", "one", "0x1", " 1", "1/answer", "999999999999999999999"}
		if flag == "part" {
			invalid = append(invalid, "01", "0001", "08")
		}
		for _, value := range invalid {
			t.Run(flag+"="+value, func(t *testing.T) {
				request := setupSubmission(t, http.StatusOK, "")
				args := []string{"-q1", "-p1", "--" + flag + "=" + value, "1309"}
				err := executeSubmit(t.Context(), args, strings.NewReader(""), io.Discard)
				if err == nil || !strings.Contains(err.Error(), flag+" number must be") || request.calls != 0 {
					t.Fatalf("error = %v, calls = %d", err, request.calls)
				}
			})
		}
	}
}

func TestSubmitHTTPResponses(t *testing.T) {
	for _, tt := range []struct {
		status int
		body   string
	}{
		{200, `{"correct":false}`},
		{201, `{"correct":true}`},
		{204, ""},
		{299, "unrecognized success response"},
		{400, `{"message":"invalid answer"}`},
		{401, `{"message":"unauthorized"}`},
		{403, "forbidden"},
		{404, ""},
		{409, `{"message":"already solved"}`},
		{409, ""},
		{418, "unrecognized response"},
		{422, `{"message":"unavailable"}`},
		{429, `{"message":"try later"}`},
		{500, "<html>server error</html>"},
		{503, ""},
		{599, "unknown error"},
	} {
		t.Run(fmt.Sprint(tt.status, tt.body), func(t *testing.T) {
			request := setupSubmission(t, tt.status, tt.body)
			var output bytes.Buffer
			err := executeSubmit(t.Context(), []string{"-y2024", "-q1", "-p1", "1309"}, strings.NewReader(""), &output)
			if request.calls != 1 {
				t.Fatalf("made %d requests, want 1 without retries", request.calls)
			}
			if tt.status >= 200 && tt.status < 300 {
				if err != nil || !strings.Contains(output.String(), fmt.Sprintf("HTTP %d", tt.status)) || !strings.Contains(output.String(), tt.body) {
					t.Fatalf("output = %q, error = %v", output.String(), err)
				}
				return
			}
			var status *everybodycodes.HTTPStatusError
			if !errors.As(err, &status) || status.StatusCode != tt.status || status.Method != "POST" {
				t.Fatalf("error = %v, want POST HTTP %d", err, tt.status)
			}
			if !strings.Contains(err.Error(), tt.body) || output.Len() != 0 {
				t.Fatalf("output = %q, error = %v; want body %q on stderr", output.String(), err, tt.body)
			}
			if tt.status == 409 && !strings.Contains(err.Error(), "already solved") {
				t.Fatalf("missing already-solved explanation: %v", err)
			}
		})
	}
}

func TestSubmitCongratulationsRequiresConfirmedCorrectAnswer(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"confirmed success", 200, `{"correct":true,"globalTime":61546407550,"globalPlace":3628,"globalScore":0,"localTime":28961483188}`, true},
		{"another 2xx status", 201, `{"correct":true}`, true},
		{"incorrect answer", 200, `{"correct":false}`, false},
		{"missing confirmation", 200, `{"message":"received"}`, false},
		{"null confirmation", 200, `{"correct":null}`, false},
		{"string confirmation", 200, `{"correct":"true"}`, false},
		{"invalid JSON", 200, `{"correct":true`, false},
		{"empty response", 204, "", false},
		{"already solved", 409, `{"message":"already solved"}`, false},
		{"error with correct field", 500, `{"correct":true}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := setupSubmission(t, tt.status, tt.body)
			var output bytes.Buffer
			err := executeSubmit(t.Context(), []string{"-y2024", "-q1", "-p2", "5560"}, strings.NewReader(""), &output)
			if (err != nil) != (tt.status >= 300) {
				t.Fatalf("error = %v, status = %d", err, tt.status)
			}
			message := "Congratulations! You solved part 2 of quest 1 (2024).\n"
			if strings.Contains(output.String(), "Congratulations!") != tt.want || (tt.want && !strings.Contains(output.String(), message)) {
				t.Fatalf("output = %q, want congratulations = %t", output.String(), tt.want)
			}
			wantDownloads := 0
			if tt.want {
				wantDownloads = 3
			}
			if len(request.inputs.calls) != wantDownloads {
				t.Fatalf("download requests = %v, want %d", request.inputs.calls, wantDownloads)
			}
		})
	}
}

func TestSubmitDownloadsNewlyUnlockedInputs(t *testing.T) {
	for _, tt := range []struct {
		year        string
		quest, part int
	}{
		{"2024", 1, 1},
		{"story-1", 8, 2},
		{"2024", 1, 3},
	} {
		t.Run(fmt.Sprintf("%s/quest-%d/part-%d", tt.year, tt.quest, tt.part), func(t *testing.T) {
			request := setupSubmission(t, http.StatusOK, `{"correct":true}`)
			questDir := fmt.Sprintf("%s/quest-%02d", tt.year, tt.quest)
			for part := 1; part <= tt.part; part++ {
				writeTestFile(t, filepath.Join(filepath.Join(questDir, fmt.Sprintf("part-%02d", part)), "input.txt"), fmt.Sprintf("existing input %d", part))
			}
			if tt.part < 3 {
				next := tt.part + 1
				keysEndpoint := fmt.Sprintf("%s/event/%s/quest/%d", apiBaseURL, tt.year, tt.quest)
				inputsEndpoint := fmt.Sprintf("https://everybody.codes/assets/%s/%d/input/8.json", tt.year, tt.quest)
				request.inputs.bodies[keysEndpoint] = jsonString(t, map[string]string{fmt.Sprintf("key%d", next): testKey})
				request.inputs.bodies[inputsEndpoint] = jsonString(t, map[string]string{fmt.Sprint(next): testNotes[next-1].encoded})
			}
			var output bytes.Buffer
			args := []string{"--year", tt.year, "--quest", fmt.Sprint(tt.quest), "--part", fmt.Sprint(tt.part), "1309"}
			if err := executeSubmit(t.Context(), args, strings.NewReader(""), &output); err != nil {
				t.Fatal(err)
			}
			if request.calls != 1 {
				t.Fatalf("submission requests = %d, want 1", request.calls)
			}
			for part := 1; part <= 3; part++ {
				dir := filepath.Join(questDir, fmt.Sprintf("part-%02d", part))
				path := filepath.Join(dir, "input.txt")
				switch {
				case part <= tt.part:
					assertFile(t, path, fmt.Sprintf("existing input %d", part))
				case part == tt.part+1:
					assertFile(t, path, testNotes[part-1].plain)
				default:
					assertMissing(t, path)
				}
				assertMissing(t, filepath.Join(dir, "input"))
				assertMissing(t, filepath.Join(dir, "input-encoded.txt"))
			}
			wantDownloads := 3
			if tt.part == 3 {
				wantDownloads = 0
			}
			if len(request.inputs.calls) != wantDownloads {
				t.Fatalf("download requests = %v, want %d", request.inputs.calls, wantDownloads)
			}
		})
	}
}

func TestSubmitReportsDownloadFailureAfterAcceptedAnswer(t *testing.T) {
	request := setupSubmission(t, http.StatusOK, `{"correct":true}`)
	request.inputs.statuses[inputsURL] = http.StatusServiceUnavailable
	path := "2024/quest-01/part-01/input.txt"
	writeTestFile(t, path, "existing input")
	var output bytes.Buffer
	err := executeSubmit(t.Context(), []string{"-y2024", "-q1", "-p1", "1309"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "answer accepted, but downloading quest inputs failed") {
		t.Fatalf("error = %v, want accepted-answer download error", err)
	}
	var status *everybodycodes.HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("error = %v, want underlying HTTP 503", err)
	}
	if request.calls != 1 || !strings.Contains(output.String(), "Congratulations!") {
		t.Fatalf("submission requests = %d, output = %q", request.calls, output.String())
	}
	assertFile(t, path, "existing input")
	assertMissing(t, "2024/quest-01/part-02/input.txt")
}

func TestSubmitRejectsRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, location := range []string{apiBaseURL + "/redirected", "https://unrelated.example/answer"} {
			t.Run(fmt.Sprint(status, location), func(t *testing.T) {
				setup(t)
				calls := 0
				apiClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{
						StatusCode: status,
						Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
						Header:     http.Header{"Location": {location}},
						Body:       io.NopCloser(strings.NewReader("redirected")),
					}, nil
				})
				err := executeSubmit(t.Context(), []string{"-q1", "-p1", "1309"}, strings.NewReader(""), io.Discard)
				var statusErr *everybodycodes.HTTPStatusError
				if !errors.As(err, &statusErr) || statusErr.StatusCode != status || calls != 1 {
					t.Fatalf("error = %v, calls = %d; want redirect rejected", err, calls)
				}
			})
		}
	}
}

func TestSubmitSessionFailures(t *testing.T) {
	for _, session := range []string{"", "invalid;cookie"} {
		t.Run(session, func(t *testing.T) {
			request := setupSubmission(t, http.StatusOK, "")
			writeTestFile(t, filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session"), session)
			err := executeSubmit(t.Context(), []string{"-q1", "-p1", "1309"}, strings.NewReader(""), io.Discard)
			if err == nil || request.calls != 0 || (session != "" && strings.Contains(err.Error(), session)) {
				t.Fatalf("error = %v, calls = %d", err, request.calls)
			}
		})
	}
}

func TestSubmitEnvironmentSession(t *testing.T) {
	request := setupSubmission(t, http.StatusOK, "")
	if err := os.Remove(filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERYBODY_CODES_SESSION_COOKIE", testSession)
	if err := executeSubmit(t.Context(), []string{"-q1", "-p1", "1309"}, strings.NewReader(""), io.Discard); err != nil || request.calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, request.calls)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestSubmitReadAndNetworkFailures(t *testing.T) {
	t.Run("stdin error", func(t *testing.T) {
		request := setupSubmission(t, http.StatusOK, "")
		readErr := errors.New("stdin failed")
		err := executeSubmit(t.Context(), []string{"-q1", "-p1"}, failingReader{readErr}, io.Discard)
		if !errors.Is(err, readErr) || request.calls != 0 {
			t.Fatalf("error = %v, calls = %d", err, request.calls)
		}
	})
	t.Run("network error", func(t *testing.T) {
		setup(t)
		calls := 0
		networkErr := errors.New("connection lost")
		apiClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, networkErr
		})
		err := executeSubmit(t.Context(), []string{"-q1", "-p1", "1309"}, strings.NewReader(""), io.Discard)
		if !errors.Is(err, networkErr) || calls != 1 || strings.Contains(err.Error(), testSession) {
			t.Fatalf("error = %v, calls = %d", err, calls)
		}
	})
}

func TestSubmitCancellation(t *testing.T) {
	for _, stage := range []string{"before input", "during input", "during request"} {
		t.Run(stage, func(t *testing.T) {
			setup(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			args := []string{"-q1", "-p1"}
			var input io.Reader = strings.NewReader("1309")
			switch stage {
			case "before input":
				cancel()
			case "during input":
				reader, writer := io.Pipe()
				defer reader.Close()
				defer writer.Close()
				input = reader
				time.AfterFunc(10*time.Millisecond, cancel)
			case "during request":
				args = append(args, "1309")
			}
			calls := 0
			apiClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				cancel()
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			err := executeSubmit(ctx, args, input, io.Discard)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context cancellation", err)
			}
			if stage != "during request" && calls != 0 {
				t.Fatalf("made %d requests despite input cancellation", calls)
			}
		})
	}
}

func TestSubmitResponseLimitsAndRedaction(t *testing.T) {
	for _, status := range []int{200, 409} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			setupSubmission(t, status, `{"message":"`+testSession+`"}`)
			var output bytes.Buffer
			err := executeSubmit(t.Context(), []string{"-q1", "-p1", "1309"}, strings.NewReader(""), &output)
			text := output.String() + fmt.Sprint(err)
			if strings.Contains(text, testSession) || !strings.Contains(text, "[REDACTED]") {
				t.Fatal("response was not redacted")
			}
		})
	}
	for _, status := range []int{200, 500} {
		t.Run(fmt.Sprintf("oversized %d", status), func(t *testing.T) {
			request := setupSubmission(t, status, strings.Repeat("x", maxJSONResponseBytes+1))
			var output bytes.Buffer
			err := executeSubmit(t.Context(), []string{"-q1", "-p1", "1309"}, strings.NewReader(""), &output)
			if err == nil || !strings.Contains(err.Error(), "exceeds") || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) || output.Len() != 0 || request.calls != 1 {
				t.Fatalf("error = %v, output length = %d, requests = %d", err, output.Len(), request.calls)
			}
		})
	}
}
