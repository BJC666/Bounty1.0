class Bounty < Formula
  desc "General-purpose AI agent framework"
  homepage "https://github.com/bounty/bounty"
  version "0.2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/bounty/bounty/releases/download/v0.2.0/bounty-darwin-arm64.tar.gz"
      sha256 "REPLACE_WITH_ACTUAL_SHA256"
    else
      url "https://github.com/bounty/bounty/releases/download/v0.2.0/bounty-darwin-amd64.tar.gz"
      sha256 "REPLACE_WITH_ACTUAL_SHA256"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/bounty/bounty/releases/download/v0.2.0/bounty-linux-arm64.tar.gz"
      sha256 "REPLACE_WITH_ACTUAL_SHA256"
    else
      url "https://github.com/bounty/bounty/releases/download/v0.2.0/bounty-linux-amd64.tar.gz"
      sha256 "REPLACE_WITH_ACTUAL_SHA256"
    end
  end

  def install
    bin.install "bounty"
  end

  test do
    system "#{bin}/bounty", "doctor"
  end
end
