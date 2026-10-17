// The native Go sources of the self-hosted compiler are compiled into the
// generated program by `ark build`. This file keeps them out of the
// repository's Go module (they import the runtime as arkprog/rt).
module arkprog/native

go 1.22
