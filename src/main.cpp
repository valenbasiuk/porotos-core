// Porotos 2 Core
// - Redirige el juego al server de Porotos 2.
// - Oculta los niveles main y The Tower (el server igual no los cuenta).
// - Al completar un nivel online, manda el puntaje para que el server sume las estrellas/lunas.

#include <Geode/Geode.hpp>
#include <Geode/modify/CCHttpClient.hpp>
#include <Geode/modify/MenuLayer.hpp>
#include <Geode/modify/LevelPage.hpp>
#include <Geode/modify/LevelAreaInnerLayer.hpp>
#include <Geode/modify/PlayLayer.hpp>

using namespace geode::prelude;

namespace {
	constexpr std::string_view kRobtopHosts[] = {
		"https://www.boomlings.com/database",
		"http://www.boomlings.com/database",
		"https://boomlings.com/database",
		"http://boomlings.com/database",
	};

	std::string serverUrl() {
		auto url = Mod::get()->getSettingValue<std::string>("server-url");
		while (!url.empty() && url.back() == '/') url.pop_back();
		return url;
	}

	// Con GDPS Switcher instalado (GD normal), el Switcher ya elige el server:
	// este mod no redirige ni oculta los main, solo manda los completados.
	bool switcherMode() {
		return Loader::get()->isModLoaded("km7dev.gdps-switcher");
	}

	// true cuando el juego esta hablando con el server de Porotos 2
	// (por este mod o por GDPS Switcher). Con el server de RobTop queda en false
	// y el mod no manda nada.
	bool g_onPorotos = false;

	bool hideMain() {
		return !switcherMode() && Mod::get()->getSettingValue<bool>("hide-main-levels");
	}

	void showNoMainAlert() {
		FLAlertLayer::create(
			"Porotos 2",
			"Aca no hay niveles main: no dan estrellas, lunas ni monedas.\nJuga los niveles <cy>online</c>!",
			"OK"
		)->show();
	}

	bool isOnlineLevel(GJGameLevel* level) {
		if (!level) return false;
		auto type = level->m_levelType;
		// Main = niveles oficiales y The Tower; Editor = niveles propios sin subir.
		if (type == GJLevelType::Main || type == GJLevelType::Editor) return false;
		return level->m_levelID.value() > 0;
	}
}

// 1) Todas las requests que van a RobTop se mandan a Porotos 2.
class $modify(PorotosHttp, CCHttpClient) {
	void send(CCHttpRequest* request) {
		if (request) {
			std::string url = request->getUrl();
			if (!switcherMode()) {
				for (auto host : kRobtopHosts) {
					if (url.rfind(host, 0) == 0) {
						url = serverUrl() + url.substr(host.size());
						request->setUrl(url.c_str());
						break;
					}
				}
			}
			if (url.rfind(serverUrl(), 0) == 0) g_onPorotos = true;
			else if (url.find("boomlings.com") != std::string::npos) g_onPorotos = false;
		}
		CCHttpClient::send(request);
	}
};

// 2) El boton Play del menu abre los niveles online en vez de los main.
class $modify(PorotosMenu, MenuLayer) {
	void onPlay(CCObject* sender) {
		if (!hideMain()) return MenuLayer::onPlay(sender);
		CCDirector::get()->pushScene(CCTransitionFade::create(0.5f, CreatorLayer::scene()));
	}
};

// 3) Si igual se llega a un nivel main, no se puede jugar.
class $modify(PorotosLevelPage, LevelPage) {
	void onPlay(CCObject* sender) {
		if (!hideMain()) return LevelPage::onPlay(sender);
		showNoMainAlert();
	}
};

// 4) The Tower tambien queda bloqueada.
class $modify(PorotosTower, LevelAreaInnerLayer) {
	void onDoor(CCObject* sender) {
		if (!hideMain()) return LevelAreaInnerLayer::onDoor(sender);
		showNoMainAlert();
	}
};

// 5) Al completar un nivel online, se manda el puntaje al server.
//    El juego normalmente lo manda solo al abrir el leaderboard del nivel;
//    asi el server se entera en el momento y recalcula el perfil.
class $modify(PorotosPlay, PlayLayer) {
	void levelComplete() {
		PlayLayer::levelComplete();

		if (!Mod::get()->getSettingValue<bool>("auto-submit")) return;
		if (!g_onPorotos) return; // nunca toca los servers de RobTop
		if (!isOnlineLevel(m_level)) return;
		if (!GJAccountManager::get() || GJAccountManager::get()->m_accountID <= 0) return;

		auto glm = GameLevelManager::get();
		Ref<GJGameLevel> level = m_level;
		// Se espera un momento para que el juego guarde el nuevo mejor porcentaje/tiempo.
		Loader::get()->queueInMainThread([glm, level] {
			glm->getLevelLeaderboard(level, LevelLeaderboardType::Global, LevelLeaderboardMode::Time);
			glm->updateUserScore();
		});
	}
};
