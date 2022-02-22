{
  description = "oasis-core-tools";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    rust-overlay.url = "github:oxalica/rust-overlay";
    flake-utils.url  = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, rust-overlay, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        overlays = [ (import rust-overlay) ];
        pkgs = import nixpkgs {
          inherit system overlays;
        };
      in
      with pkgs;
      {
        defaultPackage = rustPlatform.buildRustPackage rec {
          pname = "oasis-core-tools";
          version = "0.0.0";

          src = fetchFromGitHub {
            owner = "oasisprotocol";
            repo = "oasis-core";
            rev = "d81cce29e46d77370d06ed3551e313d6d4d988d2";
            sha256 = "sha256-aOo45LjIpsoNbFdMrIByJuRjCsuVtn7reGz0wdO011Y=";
          };

          cargoSha256 = "sha256-E4MeWeVvw/ehf1d1B3qvzYUN9yB1Yve51e8VUJBGQPs=";

          nativeBuildInputs = [
            rust-bin.nightly."2021-11-04".default
          ];

          cargoBuildFlags = [ "--package oasis-core-tools" ];
          cargoTestFlags = [ "--package oasis-core-tools" ];
        };
      }
    );
}
