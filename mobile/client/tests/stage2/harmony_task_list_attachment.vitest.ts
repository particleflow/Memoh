    // Phone-specific equivalent of the upstream direct-anchor assertions.
    // Keep the real task/file components and save bridge; mock only the OS reply.
    expect(document.querySelector('[role=dialog]')!.querySelectorAll('a')).toHaveLength(0)
    const downloads = [...document.querySelector('[role=dialog]')!.querySelectorAll('button')]
      .filter(item => item.textContent?.trim() === en.bots.files.download)
    expect(downloads).toHaveLength(1)
    const commands: Record<string, unknown>[] = []
    client.setConfig({ baseUrl: 'https://api.example.test/api', headers: { authorization: 'Bearer synthetic-identity' } })
    vi.stubGlobal('begoniaNative', { request: vi.fn(async (raw: string) => {
      const command = JSON.parse(raw) as Record<string, unknown>
      commands.push(command)
      expect(command.version).toBe(1)
      expect(command.botId).toBe('bot')
      if (command.command === 'download.fetch') {
        expect(command.url).toBe(`https://api.example.test/api/bots/bot/media/${'a'.repeat(64)}`)
        expect(command.authorization).toBe('Bearer synthetic-identity')
        expect(command.hash).toBe('a'.repeat(64))
        expect(command.size).toBe(42)
        expect(command.name).toBe('ready-result')
        return JSON.stringify({ ok: true, data: { operation: command.operation, hash: command.hash, size: command.size } })
      }
      expect(['download.save', 'download.release']).toContain(command.command)
      expect(command.operation).toBe(commands[0]?.operation)
      return JSON.stringify({ ok: true, data: true })
    }) })
    expect(commands).toHaveLength(0)
    downloads[0]!.click()
    await settled()
    expect(commands.map(command => command.command)).toEqual(['download.fetch', 'download.save', 'download.release'])
