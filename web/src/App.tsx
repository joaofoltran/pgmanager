import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { Layout } from "./components/layout/Layout";
import { ClustersPage } from "./pages/ClustersPage";
import { ClusterDetailPage } from "./pages/ClusterDetailPage";
import { MigrationsListPage } from "./pages/MigrationsListPage";
import { CreateMigrationPage } from "./pages/CreateMigrationPage";
import { MigrationDetailPage } from "./pages/MigrationDetailPage";
import { BackupPage } from "./pages/BackupPage";
import { MonitoringPage } from "./pages/MonitoringPage";
import { StandbyPage } from "./pages/StandbyPage";
import { SettingsPage } from "./pages/SettingsPage";

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<Navigate to="/clusters" replace />} />
          <Route path="/clusters" element={<ClustersPage />} />
          <Route path="/clusters/:id" element={<ClusterDetailPage />} />
          <Route path="/migration" element={<MigrationsListPage />} />
          <Route path="/migration/new" element={<CreateMigrationPage />} />
          <Route path="/migration/:id" element={<MigrationDetailPage />} />
          <Route path="/backup" element={<BackupPage />} />
          <Route path="/monitoring" element={<MonitoringPage />} />
          <Route path="/standby" element={<StandbyPage />} />
          <Route path="/settings" element={<SettingsPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
