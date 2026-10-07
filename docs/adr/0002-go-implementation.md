# ADR 0002: Go implementation

- **Status:** Accepted (decision not implemented)

## Context

The planned product is a Linux environment-management application with a future `cdots` binary and possible distribution through `go install` and GitHub Release binaries.

## Decision

Implement the application in Go, without selecting libraries or third-party packages in this decision.

## Alternatives

Conceptual alternatives are another implementation language or selecting an application framework and dependency set as part of this choice. These contrasts do not describe historical evaluation.

## Rationale

Go is the settled implementation-language choice and aligns with the intended binary-oriented distribution. No claim is made here about language-specific capabilities or existing code.

## Consequences

Future application code is intended to use Go. TUI/CLI libraries, serialization, dependency choices, package boundaries, builds, and release automation remain unselected or unimplemented. This decision does not authorize dependency installation or imply a runnable binary.
