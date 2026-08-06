import type { ReactNode } from "react";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";

export function PanelPage({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div data-panel-root className={cn("min-h-svh bg-background text-foreground", className)}>
      {children}
    </div>
  );
}

export function PanelMain({
  children,
  className,
  width = "wide"
}: {
  children: ReactNode;
  className?: string;
  width?: "default" | "wide" | "reading";
}) {
  return (
    <main
      className={cn(
        "mx-auto flex w-full flex-col gap-10 px-5 py-10 sm:px-8 sm:py-12 lg:gap-12 lg:px-10 lg:py-16",
        width === "default" && "max-w-6xl",
        width === "wide" && "max-w-[1440px]",
        width === "reading" && "max-w-5xl",
        className
      )}
    >
      {children}
    </main>
  );
}

export function PanelHeader({
  eyebrow,
  title,
  description,
  actions,
  aside,
  className
}: {
  eyebrow: string;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  aside?: ReactNode;
  className?: string;
}) {
  return (
    <header
      className={cn(
        "grid gap-8 border-b pb-10 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end lg:pb-12",
        className
      )}
    >
      <div className="flex min-w-0 flex-col gap-5">
        <div className="flex flex-col gap-3">
          <PanelEyebrow>{eyebrow}</PanelEyebrow>
          <h1 className="max-w-5xl font-heading text-4xl leading-[0.96] font-semibold tracking-[-0.045em] text-balance sm:text-5xl lg:text-6xl">
            {title}
          </h1>
        </div>
        {description ? (
          <div className="max-w-3xl text-base/relaxed text-muted-foreground text-pretty sm:text-lg/relaxed">
            {description}
          </div>
        ) : null}
        {actions ? <div className="flex flex-wrap items-center gap-3 pt-2">{actions}</div> : null}
      </div>
      {aside ? <div className="min-w-0 lg:max-w-lg">{aside}</div> : null}
    </header>
  );
}

export function PanelEyebrow({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <p className={cn("font-mono text-xs font-medium tracking-[0.16em] text-muted-foreground uppercase", className)}>
      {children}
    </p>
  );
}

export function MetricStrip({
  items,
  className
}: {
  items: Array<{ label: string; value: ReactNode; detail?: ReactNode }>;
  className?: string;
}) {
  return (
    <dl className={cn("grid border border-foreground/15 bg-card sm:grid-cols-2", className)}>
      {items.map((item, index) => (
        <div
          className={cn(
            "flex min-w-0 flex-col gap-3 p-5 sm:p-6",
            index > 0 && "border-t sm:border-t-0 sm:border-l",
            index > 1 && "sm:border-t"
          )}
          key={`${item.label}-${index}`}
        >
          <dt className="font-mono text-[0.72rem] tracking-[0.12em] text-muted-foreground uppercase">
            {item.label}
          </dt>
          <dd className="font-heading text-2xl leading-tight font-semibold tracking-[-0.035em] sm:text-3xl">{item.value}</dd>
          {item.detail ? <span className="text-sm/relaxed text-muted-foreground">{item.detail}</span> : null}
        </div>
      ))}
    </dl>
  );
}

export function SectionIntro({
  eyebrow,
  title,
  description,
  actions,
  className
}: {
  eyebrow?: string;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col gap-5 border-b px-5 pb-5 sm:flex-row sm:items-end sm:justify-between sm:px-6 sm:pb-6", className)}>
      <div className="flex min-w-0 flex-col gap-3">
        {eyebrow ? <PanelEyebrow>{eyebrow}</PanelEyebrow> : null}
        <h2 className="font-heading text-2xl font-semibold tracking-[-0.04em] text-balance sm:text-3xl">{title}</h2>
        {description ? <p className="max-w-3xl text-sm/relaxed text-muted-foreground text-pretty sm:text-base/relaxed">{description}</p> : null}
      </div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-3">{actions}</div> : null}
    </div>
  );
}

export function MonoValue({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <code className={cn("break-all font-mono text-xs text-muted-foreground", className)}>
      {children}
    </code>
  );
}

export function DetailRow({
  label,
  value,
  className
}: {
  label: ReactNode;
  value: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("grid min-w-0 gap-2 border-t py-4 first:border-t-0 sm:grid-cols-[11rem_minmax(0,1fr)] sm:gap-5", className)}>
      <dt className="font-mono text-[0.72rem] tracking-[0.1em] text-muted-foreground uppercase">{label}</dt>
      <dd className="min-w-0 text-sm/relaxed text-foreground">{value}</dd>
    </div>
  );
}

export function PanelDivider({ label }: { label?: string }) {
  return (
    <div className="flex items-center gap-4 py-2" aria-hidden={!label}>
      <Separator className="flex-1" />
      {label ? <span className="font-mono text-[0.72rem] tracking-[0.12em] text-muted-foreground uppercase">{label}</span> : null}
      <Separator className="flex-1" />
    </div>
  );
}
