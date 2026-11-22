package execx

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Fake is a Runner for tests. It records every command and answers from
// Responses, matched by the command line's prefix; unmatched commands succeed
// with no output.
type Fake struct {
	mu        sync.Mutex
	Calls     []Cmd
	Responses []FakeResponse
}

// FakeResponse answers commands whose String() starts with Prefix.
type FakeResponse struct {
	Prefix string
	Result Result
	Err    error
}

// On adds a response for commands starting with prefix.
func (f *Fake) On(prefix string, stdout string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Responses = append(f.Responses, FakeResponse{Prefix: prefix, Result: Result{Stdout: []byte(stdout)}, Err: err})
}

func (f *Fake) Run(_ context.Context, c Cmd) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, c)
	line := c.String()
	for _, r := range f.Responses {
		if strings.HasPrefix(line, r.Prefix) {
			if c.Stdout != nil {
				fmt.Fprint(c.Stdout, string(r.Result.Stdout))
			}
			return r.Result, r.Err
		}
	}
	return Result{}, nil
}

// Commands returns every recorded command line, for assertions.
func (f *Fake) Commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		out[i] = c.String()
	}
	return out
}
