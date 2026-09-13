# L018 conan probe fixture — revision 2 of the same ref (content differs from
# conanfile.py only in this comment + package payload marker => distinct rrev).
try:
    from conan import ConanFile
except ImportError:
    from conans import ConanFile
import os


class Hello18Conan(ConanFile):
    settings = "os", "arch"

    def package(self):
        dst = os.path.join(self.package_folder, "license")
        os.makedirs(dst, exist_ok=True)
        with open(os.path.join(dst, "LICENSE.txt"), "w") as f:
            f.write("l018 probe license payload r2\n")
