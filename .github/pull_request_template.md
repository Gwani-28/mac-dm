<!-- Thanks for contributing to Mac DM! -->

## What does this change?

<!-- A short description of the change and why. Link any related issue. -->

Closes #

## How was it verified?

- [ ] `go build ./... && go vet ./... && go test ./...` pass
- [ ] If the extension changed: `cd extension && npx tsc` passes
- [ ] Manually tested the affected surface (CLI / GUI / extension)

<!-- Describe the manual test steps and results. -->

## Notes

- [ ] The Go engine still has **no third-party module dependencies**
- [ ] Download logic stays in the daemon/engine (not the GUI or extension)
