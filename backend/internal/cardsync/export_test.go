package cardsync

// Test hooks: pointers to the unexported knobs so the external cardsync_test
// package can redirect the download spool directory and shorten the idle
// deadline without widening the public Options surface.

// TempFileDir points downloadToTemp's spool directory at a test scratch dir so
// a test can assert the spool file is cleaned up (CARD-013).
var TempFileDir = &tempFileDir

// DownloadIdleTimeout is the bulk-download idle deadline (CARD-015), shortened
// by tests that exercise a stalled connection.
var DownloadIdleTimeout = &downloadIdleTimeout
