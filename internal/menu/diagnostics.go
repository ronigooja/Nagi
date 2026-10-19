package menu

import "github.com/ronigooja/Nagi/internal/i18n"

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
)

func init() {
	RegisterPage("service", ServiceManagementPage)
	RegisterPage("maintenance", MaintenancePage)
	RegisterPage("diagnostics", DiagnosticsPage)
}

// ServicePage presents login activation separately from current engine controls.
func localizedServicePage(language i18n.Context) *Page {
	return &Page{ID: "diagnostics.ServicePage.page1", Title: menuText(language, "menu.diagnostics.service_page.start_at_login", "Start at Login"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		result := s.Fetch(ctx, "startup", "status")
		actions := []Item{}
		if enabled, known := localizedloginStartupEnabled(language, result); known {
			toggle := Item{ID: "diagnostics.ServicePage.item2", Label: menuText(language, "menu.diagnostics.service_page.enable_start_at_login", "Enable start at login"), Args: []string{"startup", "enable"}}
			if enabled {
				toggle = Item{ID: "diagnostics.ServicePage.item3", Label: menuText(language, "menu.diagnostics.service_page.disable_start_at_login", "Disable start at login"), Args: []string{"startup", "disable"}, Impact: menuText(language, "menu.diagnostics.service_page.keeps_the_current_proxy_running", "Keeps the current proxy running")}
			}
			actions = append(actions, toggle)
		}
		actions = append(actions,
			Item{ID: "diagnostics.ServicePage.item4", Label: menuText(language, "menu.diagnostics.service_page.view_details", "View details"), Args: []string{"startup", "status"}},
			Item{ID: "diagnostics.ServicePage.item5", Label: menuText(language, "menu.diagnostics.service_page.advanced_options", "Advanced options"), Next: func() *Page {
				return localizedloginStartupAdvancedPage(language)
			},
			},
		)
		return PageView{Context: localizedloginStartupSummary(language, result), Actions: actions}, nil
	}}
}

func localizedloginStartupAdvancedPage(language i18n.Context) *Page {
	return localizedactionPage(language, menuText(language, "menu.diagnostics.login_startup_advanced_page.login_startup_advanced", "Login Startup — Advanced"), Item{ID: "diagnostics.loginStartupAdvancedPage.item1", Label: menuText(language, "menu.diagnostics.login_startup_advanced_page.remove_login_startup_service", "Remove login startup service"), Args: []string{"service", "uninstall"}, Impact: menuText(language, "menu.diagnostics.login_startup_advanced_page.can_stop_mihomo_keeps_nagi_and_configuration", "Can stop mihomo; keeps Nagi and configuration")})
}

func localizedloginStartupEnabled(language i18n.Context, result Result) (bool, bool) {
	if result.Error != nil || result.Code != 0 {
		return false, false
	}
	var service map[string]json.RawMessage
	_ = json.Unmarshal(localizedmenuObject(language, result.Data)["service"], &service)
	var enabled *bool
	if json.Unmarshal(service["enabled"], &enabled) != nil || enabled == nil {
		return false, false
	}
	return *enabled, true
}

func localizedloginStartupSummary(language i18n.Context, result Result) string {
	if result.Error != nil || result.Code != 0 {
		reason := menuText(language, "menu.diagnostics.login_startup_summary.read_failed_open_view_details", "read failed; open View details")
		if result.Error != nil && result.errorText(language) != "" {
			reason = result.errorText(language)
		}
		return menuText(language, "menu.diagnostics.login_startup_summary.start_automatically_at_login_unavailable_mihomo", "Start automatically at login: unavailable | Mihomo: unavailable — ") + reason
	}
	object := localizedmenuObject(language, result.Data)
	var service, engine map[string]json.RawMessage
	_ = json.Unmarshal(object["service"], &service)
	_ = json.Unmarshal(object["engine"], &engine)
	state := func(raw json.RawMessage, yes, no string) string {
		var flag *bool
		if json.Unmarshal(raw, &flag) != nil || flag == nil {
			return menuState(language, "unknown")
		}
		if *flag {
			return yes
		}
		return no
	}
	return menuText(language, "menu.diagnostics.login_startup_summary.start_automatically_at_login", "Start automatically at login: ") + state(service["enabled"], menuText(language, "menu.diagnostics.login_startup_summary.on", "On"), menuText(language, "menu.diagnostics.login_startup_summary.off", "Off")) + menuText(language, "menu.diagnostics.login_startup_summary.mihomo", " | Mihomo: ") +
		state(engine["running"], "running", "stopped")
}

func localizedDiagnosticsPage(language i18n.Context) *Page {
	return localizeddiagnosticsPageForOS(language, runtime.GOOS)
}

func localizeddiagnosticsPageForOS(language i18n.Context, goos string) *Page {
	return &Page{ID: "diagnostics.diagnosticsPageForOS.page1", Title: menuText(language, "menu.diagnostics.diagnostics_page_for_o_s.monitoring_diagnostics", "Monitoring & Diagnostics"), Load: func(_ context.Context, _ *Session) (PageView, error) {
		items := []Item{
			{ID: "diagnostics.diagnosticsPageForOS.item2", Label: menuText(language, "menu.diagnostics.diagnostics_page_for_o_s.connections", "Connections"), Next: func() *Page {
				return localizedConnectionsPage(
					language,
				)
			},
			},
			{ID: "diagnostics.diagnosticsPageForOS.item3", Label: menuText(language, "menu.diagnostics.diagnostics_page_for_o_s.traffic", "Traffic"), Next: func() *Page {
				return localizedTrafficMonitorPage(
					language,
				)
			},
			},
			{ID: "diagnostics.diagnosticsPageForOS.item4", Label: menuText(language, "menu.diagnostics.diagnostics_page_for_o_s.logs", "Logs"), Next: func() *Page {
				return localizedLogsPage(language)
			},
			},
			{ID: "diagnostics.diagnosticsPageForOS.item5", Label: menuText(language, "menu.diagnostics.diagnostics_page_for_o_s.basic_checks", "Basic checks"), Args: []string{"doctor"}},
			{ID: "diagnostics.diagnosticsPageForOS.item6", Label: menuText(language, "menu.diagnostics.diagnostics_page_for_o_s.detailed_diagnostics", "Detailed diagnostics"), Args: []string{"diagnostics"}},
			{ID: "diagnostics.diagnosticsPageForOS.item7", Label: menuText(language, "menu.diagnostics.diagnostics_page_for_o_s.export_diagnostics", "Export diagnostics"), Action: exportDiagnostics},
		}

		return PageView{Context: menuText(language, "menu.diagnostics.diagnostics_page_for_o_s.inspect_connections_traffic_and_logs_then_run_ba", "Inspect connections, traffic and logs, then run basic checks or detailed diagnostics. Open Service for startup recovery."), Actions: items}, nil
	}}
}

func exportDiagnostics(ctx context.Context, s *Session) error {
	_, err := s.Form(ctx, []Field{{Key: "path", Label: menuText(s.language, "menu.diagnostics.export_diagnostics.destination_file_absolute_path", "Destination file (absolute path)"), Required: true, Validate: func(v string) error {
		if !filepath.IsAbs(v) {
			return fmt.Errorf("%s", menuText(s.language, "menu.diagnostics.export_diagnostics.enter_an_absolute_file_path_for_example_tmp_nagi", "enter an absolute file path (for example, /tmp/nagi-diagnostics.json)"))
		}
		return nil
	}}}, nil, func(v map[string]string) Result { return s.Run(ctx, "diagnostics", "export", v["path"]) })
	return err
}

func localizedServiceManagementPage(language i18n.Context) *Page {
	return localizedserviceManagementPageForOS(language, runtime.GOOS)
}

func localizedserviceManagementPageForOS(language i18n.Context, goos string) *Page {
	return &Page{ID: "diagnostics.serviceManagementPageForOS.page1", Title: menuText(language, "menu.diagnostics.service_management_page_for_o_s.service", "Service"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		view, err := localizedRuntimePage(language).Load(ctx, s)
		view.Actions = append(view.Actions, Item{ID: "diagnostics.serviceManagementPageForOS.item2", Label: menuText(language, "menu.diagnostics.service_management_page_for_o_s.start_at_login", "Start at Login"), Next: func() *Page {
			return localizedServicePage(
				language)
		},
		}, Item{ID: "diagnostics.serviceManagementPageForOS.item3", Label: menuText(language, "menu.diagnostics.service_management_page_for_o_s.startup_recovery", "Startup Recovery"), Next: func() *Page {
			return localizedstartupRecoveryPage(language)
		},
		})
		if goos == "darwin" {
			view.Actions = append(view.Actions, Item{ID: "diagnostics.serviceManagementPageForOS.item4", Label: menuText(language, "menu.diagnostics.service_management_page_for_o_s.privileged_helper", "Privileged helper"), Next: func() *Page { return localizedhelperPageForOS(language, goos) }})
		}
		return view, err
	}}
}

func localizedstartupRecoveryPage(language i18n.Context) *Page {
	return localizedactionPage(language, menuText(language, "menu.diagnostics.startup_recovery_page.startup_recovery", "Startup Recovery"), Item{ID: "diagnostics.startupRecoveryPage.item1", Label: menuText(language, "menu.diagnostics.startup_recovery_page.repair_startup_state", "Repair startup state"), Args: []string{"startup", "check"}},
		Item{ID: "diagnostics.startupRecoveryPage.item2", Label: menuText(language, "menu.diagnostics.startup_recovery_page.clean_stale_runtime_records", "Clean stale runtime records"), Args: []string{"recover"}, Impact: menuText(language, "menu.diagnostics.startup_recovery_page.clears_records_left_by_an_unexpected_exit", "Clears records left by an unexpected exit")},
	)
}

func localizedMaintenancePage(language i18n.Context) *Page {
	return localizedmaintenancePageForOS(language, runtime.GOOS)
}

func localizedmaintenancePageForOS(language i18n.Context, goos string) *Page {
	items := []Item{{ID: "diagnostics.maintenancePageForOS.item1", Label: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.show_version", "Show version"), Args: []string{"version"}}, {ID: "diagnostics.maintenancePageForOS.item2", Label: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.update_nagi", "Update Nagi"), Args: []string{"update"}, Impact: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.updates_nagi_mihomo_and_any_installed_app_resets", "Updates Nagi, mihomo and any installed App; resets the default profile")}}
	if goos == "darwin" {
		items = append(items,
			Item{ID: "diagnostics.maintenancePageForOS.item3", Label: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.install_menu_bar_app", "Install menu bar App"), Args: []string{"app", "install"}, Impact: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.installs_the_app_for_this_nagi_version", "Installs the App for this Nagi version")},
			Item{ID: "diagnostics.maintenancePageForOS.item4", Label: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.uninstall_menu_bar_app", "Uninstall menu bar App"), Args: []string{"app", "uninstall"}, Impact: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.quit_the_app_and_disable_its_login_item_first", "Quit the App and disable its login item first")},
		)
	}
	uninstallImpact := menuText(language, "menu.diagnostics.maintenance_page_for_o_s.removes_nagi_and_services_keeps_user_data", "Removes Nagi and services; keeps user data")
	if goos == "darwin" {
		uninstallImpact = menuText(language, "menu.diagnostics.maintenance_page_for_o_s.removes_nagi_app_helper_and_services_keeps_user", "Removes Nagi, App, helper and services; keeps user data")
	}
	items = append(items, Item{ID: "diagnostics.maintenancePageForOS.item5", Label: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.uninstall_nagi", "Uninstall Nagi"), Args: []string{"uninstall"}, Impact: uninstallImpact})
	items = append(items, Item{ID: "diagnostics.maintenancePageForOS.item6", Label: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.uninstall_nagi_and_clear_user_data", "Uninstall Nagi and clear user data"), Args: []string{"uninstall", "--purge-data"}, Impact: menuText(language, "menu.diagnostics.maintenance_page_for_o_s.permanently_deletes_all_nagi_user_data_including", "Permanently deletes all Nagi user data, including profiles and subscriptions")})
	return localizedactionPage(language, menuText(language, "menu.diagnostics.maintenance_page_for_o_s.maintenance", "Maintenance"), items...)
}

func localizedhelperPageForOS(language i18n.Context, goos string) *Page {
	return &Page{ID: "diagnostics.helperPageForOS.page1", Title: menuText(language, "menu.diagnostics.helper_page_for_o_s.privileged_helper", "Privileged helper"), Load: func(ctx context.Context, s *Session) (PageView, error) {
		if goos != "darwin" {
			return PageView{Context: menuText(language, "menu.diagnostics.helper_page_for_o_s.privileged_helper_is_available_on_macos_only", "Privileged helper is available on macOS only.")}, nil
		}
		return PageView{Context: pageStatus(ctx, s, menuText(language, "menu.diagnostics.helper_page_for_o_s.privileged_helper", "Privileged helper"), []string{"privileged-helper", "status"}, "installed", "enabled", "backend"), Actions: []Item{
			{ID: "diagnostics.helperPageForOS.item2", Label: menuText(language, "menu.diagnostics.helper_page_for_o_s.privileged_helper_details", "Privileged helper details"), Args: []string{"privileged-helper", "status"}},
			{ID: "diagnostics.helperPageForOS.item3", Label: menuText(language, "menu.diagnostics.helper_page_for_o_s.install_privileged_helper", "Install privileged helper"), Args: []string{"privileged-helper", "install"}, Impact: menuText(language, "menu.diagnostics.helper_page_for_o_s.requires_administrator_privileges", "Requires administrator privileges")},
			{ID: "diagnostics.helperPageForOS.item4", Label: menuText(language, "menu.diagnostics.helper_page_for_o_s.select_privileged_backend", "Select privileged backend"), Args: []string{"privileged-helper", "enable"}, Impact: menuText(language, "menu.diagnostics.helper_page_for_o_s.applies_on_next_start_stop_user_mihomo_first", "Applies on next start; stop user mihomo first")},
			{ID: "diagnostics.helperPageForOS.item5", Label: menuText(language, "menu.diagnostics.helper_page_for_o_s.select_user_backend", "Select user backend"), Args: []string{"privileged-helper", "disable"}, Impact: menuText(language, "menu.diagnostics.helper_page_for_o_s.applies_on_next_start_stop_privileged_mihomo_fir", "Applies on next start; stop privileged mihomo first")},
			{ID: "diagnostics.helperPageForOS.item6", Label: menuText(language, "menu.diagnostics.helper_page_for_o_s.uninstall_privileged_helper", "Uninstall privileged helper"), Args: []string{"privileged-helper", "uninstall"}, Impact: menuText(language, "menu.diagnostics.helper_page_for_o_s.stop_privileged_mihomo_and_select_the_user_backe", "Stop privileged mihomo and select the user backend first")},
		}}, nil
	}}
}
func ServicePage() *Page {
	return localizedServicePage(i18n.New(i18n.English))
}
func loginStartupAdvancedPage() *Page {
	return localizedloginStartupAdvancedPage(i18n.New(i18n.English))
}
func loginStartupEnabled(result Result) (bool, bool) {
	return localizedloginStartupEnabled(i18n.New(i18n.English), result)
}
func loginStartupSummary(result Result) string {
	return localizedloginStartupSummary(i18n.New(i18n.English), result)
}
func DiagnosticsPage() *Page {
	return localizedDiagnosticsPage(i18n.New(i18n.English))
}
func diagnosticsPageForOS(goos string) *Page {
	return localizeddiagnosticsPageForOS(i18n.New(i18n.English), goos)
}
func ServiceManagementPage() *Page {
	return localizedServiceManagementPage(i18n.New(i18n.English))
}
func serviceManagementPageForOS(goos string) *Page {
	return localizedserviceManagementPageForOS(i18n.New(i18n.English), goos)
}
func startupRecoveryPage() *Page {
	return localizedstartupRecoveryPage(i18n.New(i18n.English))
}
func MaintenancePage() *Page {
	return localizedMaintenancePage(i18n.New(i18n.English))
}
func maintenancePageForOS(goos string) *Page {
	return localizedmaintenancePageForOS(i18n.New(i18n.English), goos)
}
func helperPageForOS(
	goos string) *Page {
	return localizedhelperPageForOS(i18n.New(i18n.English), goos)
}
