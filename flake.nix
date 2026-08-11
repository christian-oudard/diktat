{
  description = "Voice dictation with Moonshine ONNX";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};

      # External CLIs the daemon shells out to.
      runtimeDeps = [
        pkgs.wtype
        pkgs.wl-clipboard
        pkgs.sway
      ];
      runtimeBin = pkgs.lib.makeBinPath runtimeDeps;

      # miniaudio dlopens these by SONAME at runtime.
      audioInputs = [
        pkgs.alsa-lib
        pkgs.libpulseaudio
        pkgs.pipewire
      ];
      audioLibs = pkgs.lib.makeLibraryPath audioInputs;

      # The store hash says whether two builds differ, not which commit they
      # came from, so stamp the revision in. A dirty tree has no rev, only
      # dirtyShortRev, and a tarball source has neither.
      gitRev = self.shortRev or self.dirtyShortRev or "unknown";
    in
    {
      packages.${system}.default = pkgs.buildGoModule {
        pname = "diktat";
        version = "0.2.0";
        src = ./.;
        vendorHash = null;
        ldflags = [ "-X main.gitRev=${gitRev}" ];
        # whisper.cpp is linked in, not shelled out to, so the model stays
        # loaded between utterances.
        buildInputs = [ pkgs.whisper-cpp ];
        subPackages = [ "cmd/diktat" ];
        nativeBuildInputs = [ pkgs.makeWrapper ];
        postInstall = ''
          for bin in $out/bin/*; do
            wrapProgram "$bin" \
              --set ONNXRUNTIME_LIB ${pkgs.onnxruntime}/lib/libonnxruntime.so \
              --set GGML_BACKEND_DIR ${pkgs.whisper-cpp}/lib \
              --prefix PATH : ${runtimeBin} \
              --suffix LD_LIBRARY_PATH : ${audioLibs}
          done
          install -Dm644 $src/completions/_diktat $out/share/zsh/site-functions/_diktat
        '';
      };

      # `go run`/`go build`/`go test` with the same runtime env the wrapper
      # sets, so the binaries work without going through `nix build`.
      devShells.${system}.default = pkgs.mkShell {
        packages = [ pkgs.go ] ++ runtimeDeps;
        buildInputs = audioInputs ++ [ pkgs.whisper-cpp ];
        ONNXRUNTIME_LIB = "${pkgs.onnxruntime}/lib/libonnxruntime.so";
        GGML_BACKEND_DIR = "${pkgs.whisper-cpp}/lib";
        LD_LIBRARY_PATH = audioLibs;
      };
    };
}
