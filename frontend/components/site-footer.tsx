"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import styles from "./site-footer.module.css";

const links = [
  { href: "/archive", label: "Archive" },
  { href: "/friends", label: "Friends" },
  { href: "/about", label: "About" },
];

export default function SiteFooter() {
  const pathname = usePathname();

  return (
    <footer id="site-footer" className={styles.footer}>
      <Link className="site-brand" href="/" aria-label="iSaber home" aria-current={pathname === "/" ? "page" : undefined}>
        iSaber
      </Link>
      <a className={styles.registration} href="https://beian.miit.gov.cn/" lang="zh-CN" target="_blank" rel="noopener noreferrer">
        渝ICP备2026030502号-1
      </a>
      <nav aria-label="Footer">
        {links.map(({ href, label }) => (
          <Link key={href} href={href} aria-current={pathname === href ? "page" : undefined}>{label}</Link>
        ))}
      </nav>
    </footer>
  );
}
