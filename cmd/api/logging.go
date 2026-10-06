package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"sick-fansubs/internal/logging"
)

// logHandlerOptions is the single level switch for the process: both sinks
// carry the same records, so there is no second place to forget.
var logHandlerOptions = &slog.HandlerOptions{Level: slog.LevelInfo}

// fileSinkHandler returns the process handler: the stderr handler plus a
// JSON handler writing the rotated file under dataDir/logs. It lives beside
// main so the dual-sink contract is testable: main decides when to attach it
// (once the data directory exists) and warns when it cannot.
//
// A sink that cannot be opened is NOT an error worth stopping for — the
// stderr handler is returned unchanged along with the error, so the caller
// reports it and serves on.
//
// The returned close function is never nil and is safe to call once.
func fileSinkHandler(dataDir string, stderr slog.Handler) (slog.Handler, func(), error) {
	writer, err := logging.NewRotatingWriter(
		filepath.Join(dataDir, logging.DirName),
		logging.DefaultMaxBytes, logging.DefaultMaxFiles, reportLogSinkFailure,
	)
	if err != nil {
		return stderr, func() {}, err
	}
	handler := slog.NewMultiHandler(stderr, slog.NewJSONHandler(writer, logHandlerOptions))
	return handler, func() { _ = writer.Close() }, nil
}

// reportLogSinkFailure writes a sink failure to stderr directly, not through
// slog: the failing writer is itself a logging channel, so routing its error
// back into the logger it feeds invites a loop. The writer calls this at most
// once per failure episode, so it cannot flood.
//
// The line is marshalled, not formatted — %q is Go quoting, not JSON, and the
// stderr stream is parsed as JSON by whatever reads the container logs.
func reportLogSinkFailure(err error) {
	line, marshalErr := json.Marshal(map[string]any{
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
		"level": "ERROR",
		"msg":   "log file sink write failed",
		"error": err.Error(),
	})
	if marshalErr != nil {
		// Marshalling a map of strings cannot fail; if it somehow does, the
		// sink failure itself is what matters and stderr gets nothing more.
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", line)
}
