{
  description = "Voice dictation with Moonshine ONNX";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};

      # HuggingFace's Xet CDN drops large transfers mid-stream, and a VPN
      # with too large an MTU makes it fail every few hundred KB. Resume
      # across interruptions so each chunk that arrives is kept, instead of
      # restarting from zero like fetchurl does.
      fetchModel =
        {
          name,
          url,
          hash,
        }:
        pkgs.runCommand name
          {
            outputHashMode = "flat";
            outputHashAlgo = "sha256";
            outputHash = hash;
            nativeBuildInputs = [ pkgs.curl ];
            SSL_CERT_FILE = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";
          }
          ''
            touch model
            for attempt in $(seq 1 200); do
              if curl --location --fail --continue-at - \
                   --retry 3 --retry-all-errors \
                   --speed-limit 1024 --speed-time 30 \
                   --output model "${url}"; then
                mv model $out
                exit 0
              fi
              echo "attempt $attempt stopped at $(stat -c%s model) bytes, resuming" >&2
              sleep 1
            done
            echo "download did not complete after 200 attempts" >&2
            exit 1
          '';

      encoderModel = fetchModel {
        name = "encoder_model.onnx";
        url = "https://huggingface.co/UsefulSensors/moonshine/resolve/main/onnx/merged/tiny/float/encoder_model.onnx";
        hash = "sha256-y79YD3A7KvITfg9tFM2H8xzGe9hYv9hxVAOpSJmC0aU=";
      };
      decoderModel = fetchModel {
        name = "decoder_model_merged.onnx";
        url = "https://huggingface.co/UsefulSensors/moonshine/resolve/main/onnx/merged/tiny/float/decoder_model_merged.onnx";
        hash = "sha256-QTHO8AtilC6c3vaREB8sx9u82CjXHu6MbEbCj9BR1ss=";
      };
      tokenizerJson = fetchModel {
        name = "tokenizer.json";
        url = "https://huggingface.co/UsefulSensors/moonshine-tiny/resolve/main/tokenizer.json";
        hash = "sha256-ZXl5NDi8T7r/+s9pkWn/U+N2nFoKD15xze6IU+gTDes=";
      };

      models = pkgs.runCommand "moonshine-tiny-models" { } ''
        mkdir -p $out
        cp ${encoderModel} $out/encoder.onnx
        cp ${decoderModel} $out/decoder.onnx
        cp ${tokenizerJson} $out/tokenizer.json
      '';

      # External CLIs the daemon shells out to.
      runtimeDeps = [
        pkgs.wtype
        pkgs.wl-clipboard
        pkgs.whisper-cpp
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
        subPackages = [
          "cmd/diktat-daemon"
          "cmd/diktat-model"
          "cmd/diktat-toggle"
          "cmd/diktat-repeat"
          "cmd/diktat-transcribe"
          "cmd/diktat-record"
        ];
        nativeBuildInputs = [ pkgs.makeWrapper ];
        postInstall = ''
          for bin in $out/bin/*; do
            wrapProgram "$bin" \
              --set ONNXRUNTIME_LIB ${pkgs.onnxruntime}/lib/libonnxruntime.so \
              --set MOONSHINE_MODEL_DIR ${models} \
              --prefix PATH : ${runtimeBin} \
              --suffix LD_LIBRARY_PATH : ${audioLibs}
          done
        '';
      };

      # `go run`/`go build`/`go test` with the same runtime env the wrapper
      # sets, so the binaries work without going through `nix build`.
      devShells.${system}.default = pkgs.mkShell {
        packages = [ pkgs.go ] ++ runtimeDeps;
        buildInputs = audioInputs;
        ONNXRUNTIME_LIB = "${pkgs.onnxruntime}/lib/libonnxruntime.so";
        MOONSHINE_MODEL_DIR = models;
        LD_LIBRARY_PATH = audioLibs;
      };
    };
}
