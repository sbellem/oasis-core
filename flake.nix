{
  description = "oasis-core-tools";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    rust-overlay = {
      url = "github:oxalica/rust-overlay";
      inputs.flake-utils.follows = "flake-utils";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = {
    self,
    nixpkgs,
    rust-overlay,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        overlays = [(import rust-overlay)];
        pkgs = import nixpkgs {
          inherit system overlays;
        };
      in
        with pkgs; {
          defaultPackage = rustPlatform.buildRustPackage rec {
            pname = "oasis-core-tools";
            version = "0.0.0";

            src = builtins.path {
              path = ./.;
              name = "${pname}-${version}";
            };

            cargoSha256 = "sha256-K8i9l/HRUovIRcKWs/YGeaw4BYKOlwVLmiJzJxrO8KY=";

            rust_toolchain = rust-bin.fromRustupToolchainFile ./rust-toolchain;

            nativeBuildInputs = [
              rust_toolchain
            ];

            cargoBuildFlags = ["--package oasis-core-tools"];
            cargoTestFlags = ["--package oasis-core-tools"];
          };
        }
    );
}
