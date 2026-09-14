# L018 conan probe fixture — dual client (conan 1.66 / conan 2.31.2) compatible.
try:
    from conan import ConanFile
except ImportError:
    from conans import ConanFile
import os


class Hello18Conan(ConanFile):
    # no name/version attributes: both clients take them from the CLI ref
    # (conan 1 errors on attribute/ref mismatch)
    settings = "os", "arch"

    def package(self):
        dst = os.path.join(self.package_folder, "license")
        os.makedirs(dst, exist_ok=True)
        with open(os.path.join(dst, "LICENSE.txt"), "w") as f:
            f.write("l018 probe license payload r1\n")
