import type { Metadata } from "next";
import { headers } from "next/headers";
import { Inter } from "next/font/google";
import "./globals.css";
import { WebsiteAnalyticsListener } from "./website-analytics-listener";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
  display: "swap",
});

export async function generateMetadata(): Promise<Metadata> {
  const requestHeaders = await headers();
  const host =
    requestHeaders.get("x-forwarded-host") ??
    requestHeaders.get("host") ??
    "localhost:3000";
  const protocol =
    requestHeaders.get("x-forwarded-proto") ??
    (host.startsWith("localhost") ? "http" : "https");
  const origin = `${protocol}://${host}`;
  const title = "HumanSH — Plain English in your terminal";
  const description =
    "Use plain English in the terminal you already know. HumanSH works inside your existing Zsh or Bash prompt—no replacement terminal or unfamiliar interface.";

  return {
    title,
    description,
    metadataBase: new URL(origin),
    alternates: { canonical: origin },
    openGraph: {
      title: "HumanSH — Plain English in the terminal you already use.",
      description:
        "No replacement terminal. Describe, review, and run commands in your existing prompt.",
      type: "website",
      url: origin,
      images: [
        {
          url: `${origin}/og.png`,
          width: 1200,
          height: 630,
          alt: "HumanSH turns a plain-English request into a reviewable terminal command.",
        },
      ],
    },
    twitter: {
      card: "summary_large_image",
      title: "HumanSH — Plain English in the terminal you already use.",
      description:
        "No replacement terminal. Describe, review, and run commands in your existing prompt.",
      images: [`${origin}/og.png`],
    },
  };
}

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <head>
        <link rel="shortcut icon" href="/favicon.ico?v=2" />
        <link
          rel="icon"
          href="/favicon-32.png?v=2"
          type="image/png"
          sizes="32x32"
        />
        <link
          rel="icon"
          href="/favicon.png?v=2"
          type="image/png"
          sizes="512x512"
        />
        <link
          rel="apple-touch-icon"
          href="/apple-touch-icon.png?v=2"
          type="image/png"
          sizes="180x180"
        />
      </head>
      <body className={inter.variable}>
        {children}
        <WebsiteAnalyticsListener />
      </body>
    </html>
  );
}
