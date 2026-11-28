#!/usr/bin/env node
"use strict";

const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const files = new Set(execFileSync("git", [
    "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "*.md",
], { cwd: root, encoding: "utf8" }).split("\0").filter(Boolean));
const markdown = new Map();
const inlineLinks = /\]\(\s*(?:<([^>]+)>|((?:\\.|[^\s()])+))(?:\s+["'][^\n]*["'])?\s*\)/g;
const referenceLinks = /^ {0,3}\[[^\]\n]+\]:\s*(?:<([^>]+)>|(\S+))/gm;

function prose(file) {
    if (!markdown.has(file)) {
        const source = fs.readFileSync(file, "utf8");
        markdown.set(file, source.replace(/^ {0,3}(`{3,}|~{3,}).*\n[\s\S]*?^ {0,3}\1[ \t]*$/gm, "")
            .replace(/<!--[^]*?-->/g, ""));
    }
    return markdown.get(file);
}

function anchors(file) {
    const ids = new Set();
    for (const [, id] of prose(file).matchAll(/\b(?:id|name)=["']([^"']+)["']/g)) ids.add(id);
    for (const [, heading] of prose(file).matchAll(/^ {0,3}#{1,6}\s+(.+)$/gm)) {
        const base = heading.trim().toLowerCase().replace(/[^\p{L}\p{N}\p{M}\p{Pc} -]/gu, "").replaceAll(" ", "-");
        let id = base;
        for (let suffix = 1; ids.has(id); suffix++) id = `${base}-${suffix}`;
        ids.add(id);
    }
    return ids;
}

function resolves(file, href) {
    if (/^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(href)) return true;
    try {
        const [pathname, fragment] = href.split("#", 2);
        const relative = decodeURIComponent(pathname.split("?")[0]);
        const target = relative.startsWith("/") ? path.join(root, relative)
            : relative ? path.resolve(path.dirname(file), relative) : file;
        return fs.existsSync(target) && (!fragment || !target.endsWith(".md") ||
            anchors(target).has(decodeURIComponent(fragment)));
    } catch {
        return false;
    }
}

for (const name of files) {
    const file = path.join(root, name);
    if (!fs.existsSync(file)) continue;
    for (const pattern of [inlineLinks, referenceLinks]) {
        for (const [, bracketed, plain] of prose(file).replace(/(`+)[^\n]*?\1/g, "").matchAll(pattern)) {
            const href = bracketed || plain;
            if (resolves(file, href)) continue;
            console.error(`${name}: unresolved link ${href}`);
            process.exitCode = 1;
        }
    }
}
if (!process.exitCode) console.log(`Checked relative Markdown links in ${files.size} files.`);
