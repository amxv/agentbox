"use client";

import { useRouter } from "next/navigation";

type InboxButtonProps = {
  className?: string;
  label?: string;
};

export function InboxButton({ className = "button button--solid", label = "View inbox" }: InboxButtonProps) {
  const router = useRouter();

  return (
    <button className={className} type="button" onClick={() => router.push("/threads")}>
      {label}
    </button>
  );
}
