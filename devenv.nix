{ pkgs, ... }:

{
  # Go toolchain plus the tools used by CI.
  languages.go.enable = true;

  packages = [
    pkgs.git
    pkgs.golangci-lint
  ];

  # Integration tests talk to a real Redis on localhost:6379, which is also
  # goworker's default -uri. `devenv up` starts it; `devenv test` starts it
  # automatically before running enterTest.
  services.redis = {
    enable = true;
    port = 6379;
  };

  env.REDIS_URL = "redis://localhost:6379/";

  scripts = {
    fmt.exec = ''
      gofmt -s -w .
    '';
    lint.exec = ''
      golangci-lint run ./...
    '';
    unit.exec = ''
      go test -race -count=1 ./...
    '';
  };

  enterShell = ''
    go version
  '';

  enterTest = ''
    wait_for_port 6379
    test -z "$(gofmt -s -l .)" || { gofmt -s -l .; echo "gofmt needed"; exit 1; }
    go vet ./...
    go test -race -count=1 ./...
  '';
}
