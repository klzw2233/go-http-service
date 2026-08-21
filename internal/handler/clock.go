package handler

import "time"

// now returns the current time in UTC.
//
// It is a variable rather than a direct time.Now call so tests can pin it
// and assert on exact timestamps. Previously time.Now was inlined in the
// handlers, which is why the tests could only check that echoed_at
// existed and never what it contained.
//
// Tests must restore the original value; see withFixedTime in the tests.
var now = func() time.Time { return time.Now().UTC() }
