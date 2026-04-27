import { useState } from 'react'
import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { BarChart3, Settings, User } from 'lucide-react'

import { cn } from './lib/utils'
import { GlobalProbabilityPage } from './pages/GlobalProbability'
import { SystemConfigPage } from './pages/SystemConfig'
import UserManagement from './pages/UserManagement'
import Login from './pages/Login'
import PrivateRoute from './components/PrivateRoute'
import { Toaster } from './components/ui/toaster'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "./components/ui/alert-dialog"

const sidebarSections = [
  {
    title: 'Overview',
    items: [
      { to: '/dashboard', label: 'Dashboard', icon: BarChart3 },
    ]
  },
  {
    title: 'Admin',
    items: [
      { to: '/user-management', label: 'Users', icon: User },
      { to: '/system-config', label: 'Settings', icon: Settings },
    ]
  }
]

function AppLayout() {
  const [showLogoutConfirm, setShowLogoutConfirm] = useState(false)

  const handleLogout = async () => {
    try {
      await fetch('/api/logout', { method: 'POST' });
      window.location.href = '/login';
    } catch (err) {
      console.error('Logout failed:', err);
    }
  };

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="flex h-14 items-center justify-between border-b bg-card px-4">
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/10">
            <BarChart3 className="h-4 w-4 text-primary" />
          </div>
          <div className="leading-tight">
            <div className="text-sm font-semibold">Admin Dashboard</div>
            <div className="text-xs text-muted-foreground">Management Console</div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <div
            className="flex h-9 w-9 items-center justify-center rounded-full border bg-background text-foreground cursor-pointer hover:bg-muted transition-colors"
            onClick={() => setShowLogoutConfirm(true)}
            title="Logout"
          >
            <User className="h-4 w-4" />
          </div>
        </div>
      </header>

      <AlertDialog open={showLogoutConfirm} onOpenChange={setShowLogoutConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirm Logout</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to log out?
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleLogout}>Logout</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <div className="flex min-h-[calc(100vh-3.5rem)]">
        <aside className="flex w-64 shrink-0 flex-col border-r bg-card p-4">
          <nav className="space-y-6">
            {sidebarSections.map((section) => (
              <div key={section.title}>
                <h4 className="mb-2 px-3 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  {section.title}
                </h4>
                <div className="space-y-1">
                  {section.items.map((it) => (
                    <NavLink
                      key={it.to}
                      to={it.to}
                      className={({ isActive }) =>
                        cn(
                          'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors',
                          isActive
                            ? 'bg-primary text-primary-foreground font-medium shadow-sm'
                            : 'text-foreground hover:bg-muted',
                        )
                      }
                    >
                      <it.icon className="h-4 w-4" />
                      <span>{it.label}</span>
                    </NavLink>
                  ))}
                </div>
              </div>
            ))}
          </nav>
          <div className="mt-auto border-t pt-4 text-xs text-muted-foreground">
            <div className="font-medium text-foreground">Admin Dashboard</div>
            <div>Management Console</div>
          </div>
        </aside>

        <main className="flex min-h-[calc(100vh-3.5rem)] flex-1 flex-col p-6 bg-slate-50">
          <div className="flex-1">
            <Routes>
              <Route path="/" element={<Navigate to="/dashboard" replace />} />
              <Route path="/dashboard" element={<GlobalProbabilityPage />} />
              <Route path="/system-config" element={<SystemConfigPage />} />
              <Route path="/user-management" element={<UserManagement />} />
              <Route path="*" element={<Navigate to="/dashboard" replace />} />
            </Routes>
          </div>
          <footer className="mt-8 border-t pt-4 text-xs text-muted-foreground">
            &copy; {new Date().getFullYear()} Admin Dashboard. All rights reserved.
          </footer>
        </main>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route
          path="/*"
          element={
            <PrivateRoute>
              <AppLayout />
            </PrivateRoute>
          }
        />
      </Routes>
      <Toaster />
    </>
  )
}
