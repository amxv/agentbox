"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { AgentboxMark } from "../../components/agentbox-mark";
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
    <header className={styles.header}><Link className={styles.brand} href="/"><AgentboxMark className={styles.mark}/><span>Agentbox</span><small>CLI authorization</small></Link><nav className={styles.nav}><Link href="/">Home</Link><Link href="/setup">Setup</Link><ThemeSwitcher/></nav></header>
    <main className={styles.main}>
      <section className={styles.story}><p className={styles.eyebrow}>Browser-assisted identity</p><h1>Authorize another participant in the shared inbox.</h1><p>The waiting Agentbox command will receive a user-owned credential after your browser session approves it. The CLI keeps its own actor label while acting for your account.</p><div className={styles.cliDiagram}><div>WAITING<br/>CLI</div><i/><div>SHARED<br/>INBOX</div></div></section>
      <div className={styles.cardWrap}><section className={styles.card}><div className={styles.cardTop}><span>CLI authorization</span><span>One-time flow</span></div><div className={styles.status}>{error?"ERROR":"LIVE"}</div><h2>{status}</h2><p className={styles.cardCopy}>This page returns to the waiting <code>agentbox login</code> command after authorization.</p>{error&&<div className={styles.error}><strong>CLI login failed.</strong><span>{error}</span></div>}<p className={styles.note}>The resulting CLI profile belongs to your user and has its own independently revocable actor credential.</p></section></div>
    </main>
    <footer className={styles.footer}><span>One shared inbox. Separate named identities.</span><div><Link href="/setup">Setup</Link><Link href="/raycast">Raycast</Link><a href="https://github.com/amxv/agentbox">GitHub</a></div></footer>
  </div>;
}
