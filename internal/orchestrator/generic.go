package orchestrator

import (
	"encoding/json"

	"github.com/ljw097/remnant/internal/store"
)

// generalPatternID marks a candidate found by patternless general discovery
// (auto_discover) rather than by matching a stored pattern. It is 0 so a
// hypothesis from it carries no pattern reference.
const generalPatternID int64 = 0

// genericSignatures are high-signal identifiers and calls that a region worth
// auditing tends to contain, across the languages REMNANT indexes. They only
// choose which regions the Hypothesis Researcher reads in general mode; the
// model still decides whether a real vulnerability is present, of any class —
// not only these. Word-boundary identifiers and dotted-call substrings are
// matched by the same signature compiler the patterns use.
var genericSignatures = []string{
	// Memory safety (C/C++).
	"strcpy", "strcat", "stpcpy", "sprintf", "vsprintf", "gets", "scanf", "sscanf",
	"memcpy", "memmove", "alloca", "realloc", "emalloc", "erealloc",
	// Command / process execution.
	"system", "popen", "execve", "execl", "execlp", "execvp", "shell_exec", "proc_open",
	"subprocess", "os.system", "ProcessBuilder", "Runtime.getRuntime",
	// Code evaluation.
	"eval", "exec(", "assert(", "Function(", "setTimeout",
	// Deserialization.
	"unserialize", "pickle.loads", "yaml.load", "readObject", "ObjectInputStream", "Marshal.load",
	// Injection sinks.
	"innerHTML", "dangerouslySetInnerHTML", "document.write", "mysql_query", "mysqli_query", "executeQuery",
	// Path / file handling with external names.
	// (Not a bare "include": as a word it matches every C #include line.)
	"fopen", "readFile", "include(", "include_once", "require_once", "LoadLibrary",
	// Crypto / randomness misuse hints.
	"md5", "mt_rand", "strcmp",
}

// generalPattern is the synthetic pattern used for general discovery. It has
// no root cause of its own: its "signatures" are the generic set, and it is
// never stored. Hypotheses it produces have a nil pattern reference.
func generalPattern() store.Pattern {
	sigs, _ := json.Marshal(genericSignatures)
	return store.Pattern{
		ID:                 generalPatternID,
		Title:              "General audit (no historical pattern)",
		VulnerabilityClass: "unspecified",
		CodeSignatures:     sigs,
	}
}
