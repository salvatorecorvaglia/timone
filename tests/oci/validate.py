"""Validate a local multi-platform OCI archive without publishing it."""
import hashlib
import io
import json
import sys
import tarfile

with tarfile.open(sys.argv[1]) as archive:
    def blob(descriptor):
        algorithm, digest = descriptor["digest"].split(":", 1)
        assert algorithm == "sha256", "unsupported digest"
        content = archive.extractfile("blobs/sha256/" + digest).read()
        assert len(content) == descriptor["size"], "invalid blob size"
        assert hashlib.sha256(content).hexdigest() == digest, "invalid blob digest"
        return content

    assert json.load(archive.extractfile("oci-layout"))["imageLayoutVersion"] == "1.0.0"
    found = set()

    def walk(descriptor):
        document = json.loads(blob(descriptor))
        if "manifests" in document:
            for child in document["manifests"]:
                walk(child)
            return
        config = json.loads(blob(document["config"]))
        if descriptor.get("annotations", {}).get("vnd.docker.reference.type") == "attestation-manifest":
            for layer in document["layers"]:
                assert layer["mediaType"] == "application/vnd.in-toto+json"
                blob(layer)
            return
        platform = (config.get("os"), config.get("architecture"))
        assert platform in {("linux", "amd64"), ("linux", "arm64")}, platform
        assert platform not in found, "duplicate runtime platform"
        found.add(platform)
        assert config["config"]["Entrypoint"] == ["/usr/local/bin/timone"]
        assert config["config"]["User"] == "10001:10001"
        assert config["config"]["StopSignal"] == "SIGTERM"
        binary_header = None
        for layer in document["layers"]:
            with tarfile.open(fileobj=io.BytesIO(blob(layer)), mode="r:*") as files:
                for member in files:
                    if member.name.removeprefix("./") == "usr/local/bin/timone":
                        binary_header = files.extractfile(member).read(20)
        assert binary_header and binary_header[:4] == b"\x7fELF", "missing Go ELF binary"
        assert int.from_bytes(binary_header[18:20], "little") == {"amd64": 62, "arm64": 183}[platform[1]]
        print("Validated OCI config, layer digests and ELF:", "/".join(platform))

    for descriptor in json.load(archive.extractfile("index.json"))["manifests"]:
        walk(descriptor)
    assert found == {("linux", "amd64"), ("linux", "arm64")}, "missing platform"
