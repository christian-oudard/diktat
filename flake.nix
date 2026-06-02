{
  description = "Voice dictation with Moonshine ONNX";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};

      encoderModel = pkgs.fetchurl {
        url = "https://huggingface.co/UsefulSensors/moonshine/resolve/main/onnx/merged/tiny/float/encoder_model.onnx";
        hash = "sha256-y79YD3A7KvITfg9tFM2H8xzGe9hYv9hxVAOpSJmC0aU=";
      };
      decoderModel = pkgs.fetchurl {
        url = "https://huggingface.co/UsefulSensors/moonshine/resolve/main/onnx/merged/tiny/float/decoder_model_merged.onnx";
        hash = "sha256-QTHO8AtilC6c3vaREB8sx9u82CjXHu6MbEbCj9BR1ss=";
      };
      tokenizerJson = pkgs.fetchurl {
        url = "https://huggingface.co/UsefulSensors/moonshine-tiny/resolve/main/tokenizer.json";
        hash = "sha256-ZXl5NDi8T7r/+s9pkWn/U+N2nFoKD15xze6IU+gTDes=";
      };

      models = pkgs.runCommand "moonshine-tiny-models" { } ''
        mkdir -p $out
        cp ${encoderModel} $out/encoder.onnx
        cp ${decoderModel} $out/decoder.onnx
        cp ${tokenizerJson} $out/tokenizer.json
      '';

      runtimeBin = pkgs.lib.makeBinPath [
        pkgs.wtype
        pkgs.wl-clipboard
        pkgs.sway
      ];

      # miniaudio dlopens these by SONAME at runtime.
      audioLibs = pkgs.lib.makeLibraryPath [
        pkgs.alsa-lib
        pkgs.libpulseaudio
        pkgs.pipewire
      ];
    in
    {
      packages.${system}.default = pkgs.buildGoModule {
        pname = "whisper-dictation";
        version = "0.2.0";
        src = ./.;
        vendorHash = null;
        subPackages = [
          "cmd/whisper-dictation-daemon"
          "cmd/whisper-dictation-toggle"
          "cmd/whisper-dictation-repeat"
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
    };
}
