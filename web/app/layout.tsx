import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Sparkle Transcoder",
  description: "Filesystem media scanner and transcoding task manager"
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className="dark">
      <body>{children}</body>
    </html>
  );
}
