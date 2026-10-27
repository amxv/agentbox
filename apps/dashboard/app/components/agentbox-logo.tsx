import Image from "next/image";

/** The same brand asset used in the signed-in dashboard and browser favicon. */
export function AgentboxLogo({ className }: { className?: string }) {
  return <Image className={className} src="/icon.svg" width={32} height={32} alt="" unoptimized />;
}
