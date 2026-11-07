package sink

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// ProjectInfo is the public view of a project (the token is never listed).
type ProjectInfo struct {
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
}

type project struct {
	token     string
	createdAt int64
}

// Projects is the registry of claimed project names and their tokens.
type Projects struct {
	mu       sync.RWMutex
	projects map[string]project
}

func NewProjects() *Projects { return &Projects{projects: map[string]project{}} }

// Create claims name and returns its token, or ok=false if already taken.
func (p *Projects) Create(name string) (token string, ok bool) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand failing is unrecoverable
	}
	token = hex.EncodeToString(buf)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, taken := p.projects[name]; taken {
		return "", false
	}
	p.projects[name] = project{token: token, createdAt: time.Now().UnixMilli()}
	return token, true
}

func (p *Projects) Exists(name string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.projects[name]
	return ok
}

// Verify reports whether token is the token of project name.
func (p *Projects) Verify(name, token string) bool {
	p.mu.RLock()
	pr, ok := p.projects[name]
	p.mu.RUnlock()
	return ok && subtle.ConstantTimeCompare([]byte(pr.token), []byte(token)) == 1
}

// List returns all projects sorted by name (never nil).
func (p *Projects) List() []ProjectInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ProjectInfo, 0, len(p.projects))
	for name, pr := range p.projects {
		out = append(out, ProjectInfo{Name: name, CreatedAt: pr.createdAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (p *Projects) Remove(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.projects, name)
}
