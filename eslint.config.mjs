import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = [
  ...nextVitals,
  ...nextTs,
  {
    ignores: [".next/**", ".vercel/**", "dist/**", "next-env.d.ts", "raycast/agentbox/**"]
  }
];

export default eslintConfig;
