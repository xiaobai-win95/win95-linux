package desktop

// termOptions selects what runs inside the pseudo terminal. An empty argv
// starts an interactive shell; otherwise argv runs as-is with the terminal as
// stdin/stdout/stderr, so scripts and prompts stay interactive.
type termOptions struct {
	argv []string
	cwd  string
	env  []string
}
