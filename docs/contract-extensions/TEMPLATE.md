# Contract extension proposal

## Title

Short name of the capability.

## Status

Draft | Accepted | Implemented

## Problem

What user or system need is unmet with the current contract?

## Proposal

- New or changed operations (paths, methods, messages).
- New or changed schemas (fields, types, constraints).
- Behavioral semantics (idempotency, ordering, error codes).

## Backward compatibility

Why is this safe for existing clients (MINOR), or why is MAJOR required?

## Usage pattern

Who calls what, when, and with which retry/error handling?

## Spec delta

Paste concrete OpenAPI or Protobuf fragments (or link to a branch diff in your **contract directory**, e.g. `api_spec/`).

## Rollout

1. Merge spec bump (MINOR/MAJOR).
2. Backend implements.
3. Frontend consumes regenerated SDK.
4. Integration scenarios added/updated.

## Checklist

- [ ] No silent breaking change for MINOR.
- [ ] Examples updated in the contract source.
- [ ] Integration scenarios identified.
