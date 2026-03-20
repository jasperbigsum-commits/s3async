# PR Summary
- What changed:
- Why:
- Scope:

# Test Plan
- [ ] `go test ./...`
- [ ] `go test -race ./...`
- [ ] Manual smoke test completed
- [ ] Docs updated if behavior changed

# Self Audit Checklist
## Correctness
- [ ] The change matches the intended requirement
- [ ] Error paths were reviewed
- [ ] Backward compatibility was considered

## Security
- [ ] No secrets or tokens were added
- [ ] Credential handling follows the configured security rules
- [ ] Logs do not expose sensitive information
- [ ] Input/config handling was reviewed for abuse cases

## Reliability
- [ ] Retry/timeouts were reviewed where relevant
- [ ] Failure behavior is explicit and observable
- [ ] Rollback impact is understood

## Testing
- [ ] New logic has tests or an explicit reason is documented
- [ ] Existing tests still pass
- [ ] Race-sensitive paths were considered

## Documentation / Operations
- [ ] README/docs/change-log/work-log updated as needed
- [ ] Config changes are documented
- [ ] Operational follow-up items are listed

# Follow-up TODOs
- 