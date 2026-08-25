import type { Metadata } from "next";
import { headers } from "next/headers";
import { Inter } from "next/font/google";
import "./globals.css";

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
  const title = "HumanSH — Stay in your terminal";
  const description =
    "Describe the command you need in plain English. HumanSH writes it directly into your terminal for review—no chatbot tab, no copy-paste, no lost flow.";

  return {
    title,
    description,
    metadataBase: new URL(origin),
    alternates: { canonical: origin },
    openGraph: {
      title: "HumanSH — Forgot the command? Stay in the terminal.",
      description:
        "Plain English in. Reviewable commands out. Nothing runs until you approve it.",
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
      title: "HumanSH — Forgot the command? Stay in the terminal.",
      description:
        "Plain English in. Reviewable commands out. Nothing runs until you approve it.",
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
      <body className={inter.variable}>{children}</body>
    </html>
  );
}
