const upstreamInstallerUrl =
  "https://raw.githubusercontent.com/agenticlab-ai/humansh/main/scripts/install.sh";

function installerRedirect() {
  return new Response(null, {
    status: 307,
    headers: {
      "Cache-Control": "public, max-age=300",
      Location: upstreamInstallerUrl,
      "X-Content-Type-Options": "nosniff",
    },
  });
}

export function GET() {
  return installerRedirect();
}

export function HEAD() {
  return installerRedirect();
}
