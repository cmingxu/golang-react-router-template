import { useEffect, useState } from 'react'
import { Settings, ShieldCheck } from 'lucide-react';

import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { useToast } from '../hooks/use-toast';

export function SystemConfigPage() {
  const [warnText, setWarnText] = useState('')
  const { toast } = useToast()

  useEffect(() => {
    fetch('/api/system-config')
      .then((r) => r.json())
      .then((d: { items: Record<string, string> }) => {
        setWarnText(d.items?.warn_text ?? '')
      })
      .catch((e) => {
        toast({
          title: 'Error',
          description: (e as Error).message,
          variant: 'destructive',
        })
      })
  }, [toast])

  const onSave = async () => {
    try {
      const r = await fetch('/api/system-config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key: 'warn_text', value: warnText }),
      })
      if (!r.ok) throw new Error(`HTTP ${r.status}`)

      toast({
        title: 'Success',
        description: 'Settings updated successfully.',
      })
    } catch (e) {
      toast({
        title: 'Error',
        description: (e as Error).message,
        variant: 'destructive',
      })
    }
  }

  return (
    <div className="space-y-6 pb-10">
      <div className="flex items-center gap-2 text-xl font-medium text-muted-foreground bg-white p-4 rounded-lg border shadow-sm">
        <Settings className="h-5 w-5" />
        System Settings
      </div>

      <div className="space-y-4 bg-white p-6 rounded-lg border shadow-sm max-w-4xl">
        <div className="space-y-4">
          <h2 className="flex items-center gap-2 text-base font-medium border-b pb-2">
            <ShieldCheck className="h-4 w-4 text-primary" />
            General Settings
          </h2>

          <div className="space-y-2">
            <Label>Warning Text</Label>
            <Input value={warnText} onChange={(e) => setWarnText(e.target.value)} placeholder="Enter warning text..." />
            <p className="text-sm text-muted-foreground">
              This text is displayed as a default system warning message.
            </p>
          </div>

          <Button onClick={onSave}>Save</Button>
        </div>
      </div>
    </div>
  )
}
