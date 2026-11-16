# Contributing

Thanks for helping with Plain.

By taking part you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting a bug or asking for a feature

Open an [issue](https://github.com/Gitguy268/plain/issues/new/choose) and use the template. Report security problems privately: see [SECURITY.md](SECURITY.md).

## Making a change

1. Fork the repository and create a branch from `main`.
2. Set it up:

   ```sh
   go build -o plain ./cmd/plain
   ./plain run pains/01-null/after.pln   # needs Bun or Node
   ```

3. Make your change. Keep commits small, each with a clear message.
4. Run the checks below and update the README if behaviour changed.
5. Open a pull request and fill in the template.

## Checks

```sh
gofmt -l .          # prints nothing
go vet ./...
go test ./...
```
