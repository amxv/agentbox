/// <reference types="@raycast/api">

/* 🚧 🚧 🚧
 * This file is auto-generated from the extension's manifest.
 * Do not modify manually. Instead, update the `package.json` file.
 * 🚧 🚧 🚧 */

/* eslint-disable @typescript-eslint/ban-types */

type ExtensionPreferences = {
  /** Agentbox Base URL - Base URL for the Agentbox dashboard or API proxy. */
  "baseUrl": string,
  /** Agentbox API Key - Actor API key used to authenticate Agentbox requests. */
  "apiKey": string
}

/** Preferences accessible in all the extension's commands */
declare type Preferences = ExtensionPreferences

declare namespace Preferences {
  /** Preferences accessible in the `search-threads` command */
  export type SearchThreads = ExtensionPreferences & {}
  /** Preferences accessible in the `create-thread` command */
  export type CreateThread = ExtensionPreferences & {}
  /** Preferences accessible in the `post-message` command */
  export type PostMessage = ExtensionPreferences & {}
  /** Preferences accessible in the `copy-mcp-url` command */
  export type CopyMcpUrl = ExtensionPreferences & {}
  /** Preferences accessible in the `doctor` command */
  export type Doctor = ExtensionPreferences & {}
}

declare namespace Arguments {
  /** Arguments passed to the `search-threads` command */
  export type SearchThreads = {}
  /** Arguments passed to the `create-thread` command */
  export type CreateThread = {}
  /** Arguments passed to the `post-message` command */
  export type PostMessage = {}
  /** Arguments passed to the `copy-mcp-url` command */
  export type CopyMcpUrl = {}
  /** Arguments passed to the `doctor` command */
  export type Doctor = {}
}

