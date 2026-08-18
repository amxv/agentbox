import { cn } from "@/lib/utils";

export type ConnectorBrand = "chatgpt" | "claude" | "raycast";

const logos: Record<ConnectorBrand, { src: string; label: string }> = {
  chatgpt: { src: "/brands/openai.svg", label: "OpenAI" },
  claude: { src: "/brands/claude.svg", label: "Claude" },
  raycast: { src: "/brands/raycast.svg", label: "Raycast" }
};

export function ConnectorLogo({ brand, className }: { brand: ConnectorBrand; className?: string }) {
  const logo = logos[brand];
  return (
    // These are static monochrome brand marks served from /public.
    // eslint-disable-next-line @next/next/no-img-element
    <img className={cn("panel-connector-logo", className)} src={logo.src} alt={`${logo.label} logo`} />
  );
}
