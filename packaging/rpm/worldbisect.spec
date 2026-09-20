Name:           worldbisect
Version:        1.2.1
Release:        1%{?dist}
Summary:        Git bisect for runtime reality
License:        Apache-2.0
URL:            https://github.com/ClusterPilot-System/worldbisect
Source0:        %{name}-%{version}-source.tar.gz
BuildRequires:  golang

%description
WorldBisect captures and compares Linux command executions and performs bounded,
bidirectional counterfactual experiments to isolate supported causal factors.

%prep
%autosetup -n worldbisect-%{version}

%build
make build

%install
make install DESTDIR=%{buildroot}

%files
%license LICENSE
%doc README.md CHANGELOG.md NOTICE
/usr/bin/worldbisect
/usr/bin/worldbisectd
/usr/share/man/man1/worldbisect.1
/usr/share/man/man5/worldbisect.conf.5
/usr/share/man/man8/worldbisectd.8
/usr/lib/systemd/system/worldbisectd.service
/usr/lib/sysusers.d/worldbisect.conf
/usr/lib/tmpfiles.d/worldbisect.conf

%changelog
* Sun Sep 20 2026 WorldBisect contributors - 1.2.1-1
- Defensive file-open fixes and reproducible release packaging
- Source-only team hub preview and verified CI publisher identity

* Fri Aug 21 2026 WorldBisect contributors - 1.1.1-1
- Release packaging and GitHub Action integration updates
