import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Lets the dev server serve its own assets when reached via the WSL2
  // host IP instead of localhost (Windows browser -> WSL2 next dev).
  allowedDevOrigins: ["172.18.27.65"],
};

export default nextConfig;
