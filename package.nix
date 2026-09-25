{ buildGoModule, lib }:

buildGoModule {
  pname = "hd-idle";
  version = "unstable";

  src = ./.;
  vendorHash = null;

  meta = {
    description = "Spin down idle hard disks";
    homepage = "https://github.com/adelolmo/hd-idle";
    license = lib.licenses.gpl3Plus;
    mainProgram = "hd-idle";
    platforms = lib.platforms.linux;
  };
}
