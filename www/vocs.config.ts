import { defineConfig } from 'vocs'

export default defineConfig({
  title: 'MCP-Go',
  search: {
    fuzzy: true
  },
  baseUrl: 'https://mcp-go.dev',
  basePath: '/',
  logoUrl: '/logo.png',
  description: 'A Go implementation of the Model Context Protocol (MCP), enabling seamless integration between LLM applications and external data sources and tools.',
  sidebar: [
    {
      text: 'Getting Started',
      link: '/getting-started',
    },
    {
      text: 'Quick Start',
      link: '/quick-start',
    },
    {
      text: 'Core Concepts',
      link: '/core-concepts',
    },
    {
      text: 'Building MCP Servers',
      collapsed: false,
      items: [
        {
          text: 'Overview',
          link: '/servers',
        },
        {
          text: 'Server Basics',
          link: '/servers/basics',
        },
        {
          text: 'Resources',
          link: '/servers/resources',
        },
        {
          text: 'Tools',
          link: '/servers/tools',
        },
        {
          text: 'Prompts',
          link: '/servers/prompts',
        },
        {
          text: 'Advanced Features',
          link: '/servers/advanced',
        },
      ],
    },
    {
      text: 'Transport Options',
      collapsed: false,
      items: [
        {
          text: 'Overview',
          link: '/transports',
        },
        {
          text: 'STDIO Transport',
          link: '/transports/stdio',
        },
        {
          text: 'SSE Transport',
          link: '/transports/sse',
        },
        {
          text: 'HTTP Transport',
          link: '/transports/http',
        },
        {
          text: 'In-Process Transport',
          link: '/transports/inprocess',
        },
      ],
    },
    {
      text: 'Building MCP Clients',
      collapsed: false,
      items: [
        {
          text: 'Overview',
          link: '/clients',
        },
        {
          text: 'Client Basics',
          link: '/clients/basics',
        },
        {
          text: 'Client Operations',
          link: '/clients/operations',
        },
        {
          text: 'Client Transports',
          link: '/clients/transports',
        },
      ],
    },
    {
      text: 'Advanced',
      collapsed: true,
      items: [
        {
          text: 'Server Sampling',
          link: '/servers/advanced-sampling',
        },
        {
          text: 'Client Sampling',
          link: '/clients/advanced-sampling',
        },
      ],
    },
  ],
  socials: [
    {
      icon: 'github',
      link: 'https://github.com/mark3labs/mcp-go',
    },
  ],
})
