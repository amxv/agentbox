"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { AgentboxLogo } from "../../components/agentbox-logo";
import { fetchSession } from "../../components/session";
import { ThemeSwitcher } from "../../components/theme-switcher";
import styles from "../auth.module.css";

function callbackURL(redirectURI: string, params: Record<string, string>) { const target = new URL(redirectURI); for (const [key,value] of Object.entries(params)) target.searchParams.set(key,value); return target.toString(); }

export function CLILoginView() {
  const router = useRouter(); const searchParams = useSearchParams();
  const state = searchParams.get("state") ?? ""; const redirectURI = searchParams.get("redirect_uri") ?? "";
  const [status,setStatus] = useState("Authorizing CLI access…"); const [error,setError] = useState<string|null>(null);
  const next = useMemo(()=>{ const current=`/login/cli?${searchParams.toString()}`; return `/login?next=${encodeURIComponent(current)}`; },[searchParams]);
  useEffect(()=>{ async function authorize(){ if(!state||!redirectURI){setError("Missing CLI login parameters.");setStatus("Unable to authorize CLI access.");return;} try{const session=await fetchSession();if(!session){router.replace(next);return;}const response=await fetch("/api/auth/cli/authorize",{method:"POST",headers:{"content-type":"application/json"},body:JSON.stringify({state,redirect_uri:redirectURI})});const data=await response.json() as {code?:string;error?:string};if(!response.ok||!data.code)throw new Error(data.error??`HTTP ${response.status}`);window.location.assign(callbackURL(redirectURI,{code:data.code,state}));}catch(err){const message=err instanceof Error?err.message:String(err);setError(message);setStatus("Unable to authorize CLI access.");if(redirectURI)window.location.assign(callbackURL(redirectURI,{error:message,state}));}} void authorize(); },[next,redirectURI,router,state]);
  return <div className={styles.page}>
    <header className={styles.header}><Link className={styles.brand} href="/"><AgentboxLogo className={styles.mark}/><span>Agentbox</span></Link><nav className={styles.nav} aria-label="Account navigation"><Link href="/">Home</Link><ThemeSwitcher compact/></nav></header>
    <main className={styles.main}>
      <div className={styles.cardWrap}>
        <section className={styles.card} aria-live="polite">
          <p className={styles.eyebrow}>CLI authorization</p>
          <div className={styles.status}>{error ? "!" : "↗"}</div>
          <h1>{status}</h1>
          <p className={styles.cardCopy}>You will return to the waiting <code>agentbox login</code> command automatically.</p>
          {error && <div className={styles.error} role="alert"><strong>Authorization failed.</strong><span>{error}</span></div>}
        </section>
      </div>
    </main>
    <footer className={styles.footer}><Link href="/setup">Self-host Agentbox</Link><a href="https://github.com/amxv/agentbox">GitHub</a></footer>
  </div>;
}
